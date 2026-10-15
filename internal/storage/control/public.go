package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// PublicPageConfig returns the singleton public page and its explicit node
// allowlist in stable order.
func (s *Store) PublicPageConfig(ctx context.Context) (PublicPage, error) {
	var page PublicPage
	var enabled int
	var updated int64
	if err := s.db.QueryRowContext(ctx, `SELECT enabled,title,description,updated_at FROM public_page WHERE singleton_id=1`).Scan(
		&enabled, &page.Title, &page.Description, &updated,
	); err != nil {
		return PublicPage{}, fmt.Errorf("read public page: %w", err)
	}
	page.Enabled = enabled != 0
	page.UpdatedAt = fromUnix(updated)
	rows, err := s.db.QueryContext(ctx, `SELECT p.node_id FROM public_nodes p JOIN nodes n ON n.id=p.node_id
		WHERE n.deleted_at IS NULL ORDER BY n.group_name,n.name,n.id`)
	if err != nil {
		return PublicPage{}, fmt.Errorf("list public nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return PublicPage{}, fmt.Errorf("scan public node: %w", err)
		}
		page.NodeIDs = append(page.NodeIDs, nodeID)
	}
	if err := rows.Err(); err != nil {
		return PublicPage{}, fmt.Errorf("iterate public nodes: %w", err)
	}
	return page, nil
}

// UpdatePublicPage retains the legacy complete replacement operation.
func (s *Store) UpdatePublicPage(ctx context.Context, page PublicPage, now time.Time) error {
	return s.UpdateSiteSettings(ctx, page, &page.NodeIDs, now)
}

// UpdateSiteSettings saves presentation/access settings and optionally replaces
// display membership. A nil nodeIDs preserves membership inside this transaction,
// including nodes enrolled or edited since the settings form was loaded.
func (s *Store) UpdateSiteSettings(ctx context.Context, page PublicPage, nodeIDs *[]string, now time.Time) error {
	page.Title = strings.TrimSpace(page.Title)
	page.Description = strings.TrimSpace(page.Description)
	if page.Title == "" || len(page.Title) > 128 || len(page.Description) > 512 {
		return ErrInvalidPublicConfiguration
	}
	var replacement []string
	if nodeIDs != nil {
		if len(*nodeIDs) > MaxPublicNodes {
			return ErrInvalidPublicConfiguration
		}
		replacement = append([]string(nil), (*nodeIDs)...)
	}
	sort.Strings(replacement)
	for index, nodeID := range replacement {
		if nodeID == "" || len(nodeID) > 128 || index > 0 && replacement[index-1] == nodeID {
			return ErrInvalidPublicConfiguration
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin public page update: %w", err)
	}
	defer tx.Rollback()
	for _, nodeID := range replacement {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE id=? AND deleted_at IS NULL)`, nodeID).Scan(&exists); err != nil {
			return fmt.Errorf("validate public node: %w", err)
		}
		if exists == 0 {
			return ErrInvalidPublicConfiguration
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public_page SET enabled=?,title=?,description=?,updated_at=? WHERE singleton_id=1`,
		boolInt(page.Enabled), page.Title, page.Description, unix(now)); err != nil {
		return fmt.Errorf("update public page: %w", err)
	}
	if nodeIDs != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM public_nodes`); err != nil {
			return fmt.Errorf("replace public nodes: %w", err)
		}
		for _, nodeID := range replacement {
			if _, err := tx.ExecContext(ctx, `INSERT INTO public_nodes(node_id,published_at) VALUES(?,?)`, nodeID, unix(now)); err != nil {
				return fmt.Errorf("publish node: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit public page update: %w", err)
	}
	return nil
}

// ListPublishedNodes returns only active nodes present in the server-side
// publication allowlist. Callers must still project them to a public DTO.
func (s *Store) ListPublishedNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n.id,n.name,n.group_name,n.hidden,n.hostname,n.os,n.kernel,n.architecture,n.virtualization,n.created_at,n.updated_at,1
		FROM public_nodes p JOIN nodes n ON n.id=p.node_id WHERE n.deleted_at IS NULL
		ORDER BY n.group_name,n.name,n.id`)
	if err != nil {
		return nil, fmt.Errorf("list published nodes: %w", err)
	}
	defer rows.Close()
	nodes := make([]Node, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan published node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published nodes: %w", err)
	}
	return nodes, nil
}
