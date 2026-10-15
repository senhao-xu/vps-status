package control

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
)

func TestHomepageMigrationPreservesExplicitSettingsAndMembership(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name             string
		enabled, updated int
		want             bool
	}{
		{"untouched", 0, 0, true}, {"explicit-restricted", 0, 123, false}, {"explicit-open", 1, 123, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "control.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at INTEGER NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			for _, m := range migrations[:10] {
				if _, err := db.Exec(m.sql); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?,1)`, m.version, m.name); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES('shown','Shown',1,1),('unlisted','Unlisted',1,1);
    INSERT INTO public_nodes VALUES('shown',99);`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE public_page SET enabled=?,title='Existing title',description='Existing description',updated_at=?`, tc.enabled, tc.updated); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			page, err := store.PublicPageConfig(t.Context())
			if err != nil || page.Enabled != tc.want || page.Title != "Existing title" || page.Description != "Existing description" || page.UpdatedAt.Unix() != int64(tc.updated) || len(page.NodeIDs) != 1 || page.NodeIDs[0] != "shown" {
				t.Fatalf("migrated page=%+v %v", page, err)
			}
			shown, _ := store.NodeByID(t.Context(), "shown")
			unlisted, _ := store.NodeByID(t.Context(), "unlisted")
			if !shown.HomepageVisible || unlisted.HomepageVisible {
				t.Fatalf("membership expanded: shown=%+v unlisted=%+v", shown, unlisted)
			}
			backups, err := filepath.Glob(path + ".backup-v10-*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups=%v %v", backups, err)
			}
			info, err := os.Stat(backups[0])
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("backup permissions: %v %v", info, err)
			}
			backup, err := sql.Open("sqlite", backups[0])
			if err != nil {
				t.Fatal(err)
			}
			defer backup.Close()
			var version, enabled, updated, published int
			if err := backup.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err := backup.QueryRow(`SELECT enabled,updated_at FROM public_page`).Scan(&enabled, &updated); err != nil {
				t.Fatal(err)
			}
			if err := backup.QueryRow(`SELECT published_at FROM public_nodes WHERE node_id='shown'`).Scan(&published); err != nil {
				t.Fatal(err)
			}
			if version != 10 || enabled != tc.enabled || updated != tc.updated || published != 99 {
				t.Fatalf("backup was mutated: v%d enabled=%d updated=%d published=%d", version, enabled, updated, published)
			}
		})
	}
}

func seedHomepageCapacity(t *testing.T, s *Store, count int) {
	t.Helper()
	if count == 0 {
		return
	}
	tx, err := s.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("capacity-%04d", i)
		if _, err := tx.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(?,?,1,1)`, id, id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO public_nodes VALUES(?,1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestHomepageEnrollmentCapAndAtomicNodeSettings(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	seedHomepageCapacity(t, s, MaxPublicNodes)
	createPublicTestNode(t, s, "overflow", now)
	node, err := s.NodeByID(t.Context(), "overflow")
	if err != nil || node.HomepageVisible {
		t.Fatalf("overflow enrollment=%+v %v", node, err)
	}
	if _, _, err := s.AuthenticateNodeCredential(t.Context(), security.DigestNodeCredential("credential-overflow")); err != nil {
		t.Fatalf("overflow credential=%v", err)
	}
	shown, hidden, name := true, true, "must roll back"
	if _, err := s.PatchNode(t.Context(), node.ID, NodePatch{Name: &name, Hidden: &hidden, HomepageVisible: &shown}, now); !errors.Is(err, ErrHomepageNodeLimit) {
		t.Fatalf("enable overflow=%v", err)
	}
	node, _ = s.NodeByID(t.Context(), node.ID)
	if node.Name == name || node.Hidden || node.HomepageVisible {
		t.Fatalf("partial node save=%+v", node)
	}
	shown = false
	if _, err := s.PatchNode(t.Context(), "capacity-0000", NodePatch{HomepageVisible: &shown}, now); err != nil {
		t.Fatal(err)
	}
	shown = true
	node, err = s.PatchNode(t.Context(), node.ID, NodePatch{Hidden: &hidden, HomepageVisible: &shown}, now)
	if err != nil || !node.Hidden || !node.HomepageVisible {
		t.Fatalf("independent settings=%+v %v", node, err)
	}
	if _, err := s.PatchNode(t.Context(), node.ID, NodePatch{HomepageVisible: &shown}, now); err != nil {
		t.Fatalf("unchanged true at capacity=%v", err)
	}
	hidden = false
	if _, err := s.PatchNode(t.Context(), node.ID, NodePatch{Hidden: &hidden}, now); err != nil {
		t.Fatal(err)
	}
	node, _ = s.NodeByID(t.Context(), node.ID)
	if node.Hidden || !node.HomepageVisible {
		t.Fatalf("hidden affected homepage=%+v", node)
	}
	if err := s.UpdateSiteSettings(t.Context(), PublicPage{Title: "Renamed", Enabled: false}, nil, now); err != nil {
		t.Fatal(err)
	}
	page, _ := s.PublicPageConfig(t.Context())
	if page.Enabled || page.Title != "Renamed" || len(page.NodeIDs) != MaxPublicNodes {
		t.Fatalf("settings replaced membership=%+v", page)
	}
	empty := []string{}
	if err := s.UpdateSiteSettings(t.Context(), PublicPage{Title: "Renamed", Enabled: true}, &empty, now); err != nil {
		t.Fatal(err)
	}
	page, _ = s.PublicPageConfig(t.Context())
	if len(page.NodeIDs) != 0 {
		t.Fatal("explicit clear failed")
	}
}

func TestHomepageConcurrentEnrollmentAndSettingsPreserveMembership(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	seedHomepageCapacity(t, s, MaxPublicNodes-1)
	for _, id := range []string{"first", "second"} {
		token := EnrollmentToken{ID: id, Digest: security.DigestEnrollmentToken(id), NodeName: id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := s.CreateEnrollmentToken(t.Context(), token); err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 3)
	start := make(chan struct{})
	for _, id := range []string{"first", "second"} {
		go func(id string) {
			<-start
			_, err := s.ConsumeEnrollment(t.Context(), security.DigestEnrollmentToken(id), id, "credential-"+id, security.DigestNodeCredential(id), now)
			results <- err
		}(id)
	}
	go func() {
		<-start
		results <- s.UpdateSiteSettings(t.Context(), PublicPage{Title: "Concurrent site", Enabled: true}, nil, now)
	}()
	close(start)
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	first, _ := s.NodeByID(t.Context(), "first")
	second, _ := s.NodeByID(t.Context(), "second")
	page, _ := s.PublicPageConfig(t.Context())
	if first.HomepageVisible == second.HomepageVisible || len(page.NodeIDs) != MaxPublicNodes || page.Title != "Concurrent site" {
		t.Fatalf("admission lost: first=%v second=%v members=%d title=%s", first.HomepageVisible, second.HomepageVisible, len(page.NodeIDs), page.Title)
	}
}

func TestHomepageEnrollmentFailureRollsBackMembershipAndToken(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	token := EnrollmentToken{ID: "rollback", Digest: security.DigestEnrollmentToken("rollback"), NodeName: "rollback", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_homepage BEFORE INSERT ON public_nodes BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeEnrollment(t.Context(), token.Digest, "rollback", "credential", security.DigestNodeCredential("credential"), now); err == nil {
		t.Fatal("expected injected failure")
	}
	for _, table := range []string{"nodes", "node_credentials", "public_nodes"} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d %v", table, count, err)
		}
	}
	var consumed sql.NullInt64
	if err := s.db.QueryRow(`SELECT consumed_at FROM enrollment_tokens WHERE id='rollback'`).Scan(&consumed); err != nil || consumed.Valid {
		t.Fatalf("consumed=%+v %v", consumed, err)
	}
}

func TestHomepageVisibilityAndMetadataRollbackTogether(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	createPublicTestNode(t, s, "node", now)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_node_update BEFORE UPDATE ON nodes BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	visible := false
	name := "changed"
	if _, err := s.PatchNode(t.Context(), "node", NodePatch{Name: &name, HomepageVisible: &visible}, now); err == nil {
		t.Fatal("expected injected metadata failure")
	}
	node, _ := s.NodeByID(t.Context(), "node")
	if !node.HomepageVisible || node.Name == name {
		t.Fatalf("partial metadata/membership=%+v", node)
	}
}
