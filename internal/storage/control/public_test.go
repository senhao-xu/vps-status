package control

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
)

func createPublicTestNode(t *testing.T, store *Store, id string, now time.Time) {
	t.Helper()
	token := EnrollmentToken{ID: "token-" + id, Digest: security.DigestEnrollmentToken("token-" + id),
		NodeName: "alias-" + id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, id, "credential-"+id,
		security.DigestNodeCredential("credential-"+id), now); err != nil {
		t.Fatal(err)
	}
}

func TestPublicPageDefaultsPrivateAndReplacesAllowlist(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	createPublicTestNode(t, store, "node-a", now)
	createPublicTestNode(t, store, "node-b", now)

	page, err := store.PublicPageConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if page.Enabled || page.Title != "VPS Status" || len(page.NodeIDs) != 0 {
		t.Fatalf("default public page = %#v", page)
	}
	page = PublicPage{Enabled: true, Title: "Edge status", Description: "Availability", NodeIDs: []string{"node-b", "node-a"}}
	if err := store.UpdatePublicPage(t.Context(), page, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	page, err = store.PublicPageConfig(t.Context())
	if err != nil || len(page.NodeIDs) != 2 || page.NodeIDs[0] != "node-a" || page.NodeIDs[1] != "node-b" {
		t.Fatalf("updated public page = %#v, %v", page, err)
	}
	published, err := store.ListPublishedNodes(t.Context())
	if err != nil || len(published) != 2 {
		t.Fatalf("published nodes = %#v, %v", published, err)
	}

	if err := store.UpdatePublicPage(t.Context(), PublicPage{Enabled: true, Title: "Status", NodeIDs: []string{"missing"}}, now); !errors.Is(err, ErrInvalidPublicConfiguration) {
		t.Fatalf("missing node error = %v", err)
	}
	unchanged, err := store.PublicPageConfig(t.Context())
	if err != nil || unchanged.Title != "Edge status" || len(unchanged.NodeIDs) != 2 {
		t.Fatalf("invalid update changed page = %#v, %v", unchanged, err)
	}

	if err := store.DeleteNode(t.Context(), "node-a", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	page, err = store.PublicPageConfig(t.Context())
	if err != nil || len(page.NodeIDs) != 1 || page.NodeIDs[0] != "node-b" {
		t.Fatalf("deleted publication was not cascaded = %#v, %v", page, err)
	}
	var stalePublication int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM public_nodes WHERE node_id='node-a'`).Scan(&stalePublication); err != nil {
		t.Fatal(err)
	}
	if stalePublication != 0 {
		t.Fatalf("soft-deleted node retained %d durable public allowlist rows", stalePublication)
	}
}

func TestOpenUpgradesV6WithImmutableBackupBeforePublicSchema(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "control.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 6; index++ {
		if _, err := db.Exec(migrations[index].sql); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,1)`, migrations[index].version, migrations[index].name); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(path + ".backup-v6-*")
	if len(backups) != 1 {
		t.Fatalf("v6 backup count=%d", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	for _, table := range []string{"public_page", "public_nodes"} {
		var count int
		if err := backupDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("v6 backup was mutated with v7 table %s", table)
		}
	}
}
