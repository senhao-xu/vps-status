package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func TestOpenMigratesAndRestrictsDatabase(t *testing.T) {
	store := openTestStore(t)
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("Stat(control.db) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("control.db mode = %o; want 600", got)
	}
	var version int
	if err := store.db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 8 {
		t.Fatalf("schema version = %d; want 8", version)
	}
	for _, table := range []string{"admins", "sessions", "recovery_codes", "nodes", "node_credentials", "enrollment_tokens", "telemetry_cleanup_jobs", "probe_tasks", "probe_assignments", "probe_config_state", "telemetry_probe_cleanup_jobs", "notification_channels", "alert_rules", "alert_rule_channels", "alert_states", "notification_deliveries", "public_page", "public_nodes", "appearance"} {
		var count int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d; want 1", table, count)
		}
	}
}

func TestOpenBacksUpBeforeChangingAnExistingDatabase(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "control.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE legacy_marker (value TEXT NOT NULL); INSERT INTO legacy_marker(value) VALUES ('before-migration')"); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() legacy database error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}

	backups, err := filepath.Glob(path + ".backup-v0-*")
	if err != nil {
		t.Fatalf("glob migration backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("backup count = %d; want 1", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatalf("open migration backup: %v", err)
	}
	t.Cleanup(func() { _ = backupDB.Close() })
	var marker string
	if err := backupDB.QueryRow("SELECT value FROM legacy_marker").Scan(&marker); err != nil || marker != "before-migration" {
		t.Fatalf("backup marker = %q, %v; want original row", marker, err)
	}
	var migrationTableCount int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'").Scan(&migrationTableCount); err != nil {
		t.Fatalf("inspect backup schema: %v", err)
	}
	if migrationTableCount != 0 {
		t.Fatal("pre-migration backup already contains migration metadata")
	}
}

func TestOpenRejectsNewerSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open future database: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at INTEGER NOT NULL
	    ); INSERT INTO schema_migrations(version, name, applied_at) VALUES (9, '009_future.sql', 1)`); err != nil {
		_ = db.Close()
		t.Fatalf("seed future database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close future database: %v", err)
	}

	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("Open() accepted a schema newer than this Control build")
	}
}

func TestOpenUpgradesV1WithBackup(t *testing.T) {
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
	if _, err := db.Exec(migrations[0].sql); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (1, '001_admin_security.sql', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("upgrade v1: %v", err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 8 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	backups, _ := filepath.Glob(path + ".backup-v1-*")
	if len(backups) != 1 {
		t.Fatalf("v1 backup count=%d", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	var nodesTable int
	if err := backupDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='nodes'`).Scan(&nodesTable); err != nil || nodesTable != 0 {
		t.Fatalf("backup was mutated: table=%d err=%v", nodesTable, err)
	}
}

func TestOpenUpgradesV2WithBackup(t *testing.T) {
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
	if len(migrations) < 3 {
		t.Fatalf("migration count=%d; want at least 3", len(migrations))
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := db.Exec(migrations[index].sql); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, 1)`, migrations[index].version, migrations[index].name); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("upgrade v2: %v", err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 8 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	backups, _ := filepath.Glob(path + ".backup-v2-*")
	if len(backups) != 1 {
		t.Fatalf("v2 backup count=%d", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	var groupColumn int
	if err := backupDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('nodes') WHERE name='group_name'`).Scan(&groupColumn); err != nil {
		t.Fatal(err)
	}
	if groupColumn != 0 {
		t.Fatal("v2 backup was mutated with v3 node metadata")
	}
}

func TestOpenUpgradesV3WithBackupBeforeCleanupSchema(t *testing.T) {
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
	for index := 0; index < 3; index++ {
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
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backups, _ := filepath.Glob(path + ".backup-v3-*")
	if len(backups) != 1 {
		t.Fatalf("v3 backup count=%d", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	var cleanupTable int
	if err := backupDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='telemetry_cleanup_jobs'`).Scan(&cleanupTable); err != nil {
		t.Fatal(err)
	}
	if cleanupTable != 0 {
		t.Fatal("v3 backup was mutated with v4 telemetry cleanup schema")
	}
}

func TestOpenUpgradesV4WithBackupBeforeProbeSchema(t *testing.T) {
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
	for index := 0; index < 4; index++ {
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
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backups, _ := filepath.Glob(path + ".backup-v4-*")
	if len(backups) != 1 {
		t.Fatalf("v4 backup count=%d", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	for _, table := range []string{"probe_tasks", "probe_assignments", "probe_config_state", "telemetry_probe_cleanup_jobs"} {
		var count int
		if err := backupDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("v4 backup was mutated with v5 table %s", table)
		}
	}
}

func TestOpenUpgradesV5WithImmutableBackupBeforeAlertSchema(t *testing.T) {
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
	if len(migrations) < 6 {
		t.Fatalf("migration count=%d; want at least 6", len(migrations))
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
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
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(path + ".backup-v5-*")
	if len(backups) != 1 {
		t.Fatalf("v5 backup count=%d", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	for _, table := range []string{"notification_channels", "alert_rules", "alert_rule_channels", "alert_states", "notification_deliveries"} {
		var count int
		if err := backupDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("v5 backup was mutated with v6 table %s", table)
		}
	}
}

func TestEnrollmentCredentialLifecycle(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	makeToken := func(id, plaintext string, expires time.Time) EnrollmentToken {
		return EnrollmentToken{ID: id, Digest: security.DigestEnrollmentToken(plaintext), NodeName: "node", CreatedAt: now, ExpiresAt: expires}
	}
	expired := makeToken("expired", "expired-token", now.Add(time.Minute))
	if err := store.CreateEnrollmentToken(t.Context(), expired); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), expired.Digest, "n0", "c0", security.DigestNodeCredential("c"), now.Add(2*time.Minute)); !errors.Is(err, ErrEnrollmentExpired) {
		t.Fatalf("expired error=%v", err)
	}
	valid := makeToken("valid", "valid-token", now.Add(time.Hour))
	if err := store.CreateEnrollmentToken(t.Context(), valid); err != nil {
		t.Fatal(err)
	}
	firstDigest := security.DigestNodeCredential("credential-one")
	node, err := store.ConsumeEnrollment(t.Context(), valid.Digest, "node-1", "credential-1", firstDigest, now)
	if err != nil || node.ID != "node-1" {
		t.Fatalf("consume=%#v %v", node, err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), valid.Digest, "node-2", "credential-2", firstDigest, now); !errors.Is(err, ErrEnrollmentConsumed) {
		t.Fatalf("reconsume error=%v", err)
	}
	if _, credential, err := store.AuthenticateNodeCredential(t.Context(), firstDigest); err != nil || credential.ID != "credential-1" {
		t.Fatalf("authenticate=%#v %v", credential, err)
	}
	secondDigest := security.DigestNodeCredential("credential-two")
	if err := store.RotateNodeCredential(t.Context(), node.ID, "credential-2", secondDigest, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AuthenticateNodeCredential(t.Context(), firstDigest); !errors.Is(err, ErrCredentialRevoked) {
		t.Fatalf("old credential error=%v", err)
	}
	if _, _, err := store.AuthenticateNodeCredential(t.Context(), secondDigest); err != nil {
		t.Fatalf("new credential error=%v", err)
	}
	if err := store.RevokeNodeCredentials(t.Context(), node.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AuthenticateNodeCredential(t.Context(), secondDigest); !errors.Is(err, ErrCredentialRevoked) {
		t.Fatalf("revoked credential error=%v", err)
	}
}

func TestNodeMetadataCRUDAndControlStorageBoundary(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	token := EnrollmentToken{
		ID: "node-metadata-token", Digest: security.DigestEnrollmentToken("node-metadata-token"),
		NodeName: "edge", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	credential := security.DigestNodeCredential("node-metadata-credential")
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, "node-metadata", "credential-metadata", credential, now); err != nil {
		t.Fatal(err)
	}
	facts := NodeFacts{Hostname: "real-host", OS: "Linux", Kernel: "6.8.0", Architecture: "amd64", Virtualization: "kvm"}
	if err := store.UpdateNodeFacts(t.Context(), "node-metadata", facts, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNode(t.Context(), "node-metadata", "display-name", "hk", true, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	node, err := store.NodeByID(t.Context(), "node-metadata")
	if err != nil {
		t.Fatal(err)
	}
	if node.Name != "display-name" || node.Group != "hk" || !node.Hidden || node.Facts != facts {
		t.Fatalf("node metadata=%#v", node)
	}
	nodes, err := store.ListNodes(t.Context())
	if err != nil || len(nodes) != 1 || nodes[0].ID != node.ID {
		t.Fatalf("ListNodes()=%#v, %v", nodes, err)
	}

	for _, forbidden := range []string{"report", "sample", "traffic", "probe_result", "cpu", "memory", "network_rate"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND lower(name) LIKE ?`, "%"+forbidden+"%").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("control.db contains forbidden telemetry-like table matching %q", forbidden)
		}
	}

	if err := store.DeleteNode(t.Context(), node.ID, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.ListTelemetryCleanupJobs(t.Context(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].NodeID != node.ID || jobs[0].Attempts != 0 {
		t.Fatalf("cleanup jobs=%#v, %v", jobs, err)
	}
	if err := store.MarkTelemetryCleanupAttempt(t.Context(), node.ID, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.ListTelemetryCleanupJobs(t.Context(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 || jobs[0].LastAttemptAt == nil {
		t.Fatalf("attempted cleanup jobs=%#v, %v", jobs, err)
	}
	if _, err := store.NodeByID(t.Context(), node.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted node lookup error=%v", err)
	}
	if _, _, err := store.AuthenticateNodeCredential(t.Context(), credential); !errors.Is(err, ErrCredentialRevoked) {
		t.Fatalf("deleted node credential error=%v", err)
	}
	if err := store.CompleteTelemetryCleanup(t.Context(), node.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteTelemetryCleanup(t.Context(), node.ID); err != nil {
		t.Fatalf("repeated cleanup completion: %v", err)
	}
	jobs, err = store.ListTelemetryCleanupJobs(t.Context(), 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("completed cleanup jobs=%#v, %v", jobs, err)
	}
}

func TestNodeDeletionRollsBackWhenCleanupJobCannotBeRecorded(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	token := EnrollmentToken{ID: "rollback-token", Digest: security.DigestEnrollmentToken("rollback-token"), NodeName: "node", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	credential := security.DigestNodeCredential("rollback-credential")
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, "rollback-node", "rollback-credential", credential, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP TABLE telemetry_cleanup_jobs`); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteNode(t.Context(), "rollback-node", now.Add(time.Minute)); err == nil {
		t.Fatal("node deletion succeeded without durable cleanup outbox")
	}
	if _, err := store.NodeByID(t.Context(), "rollback-node"); err != nil {
		t.Fatalf("node soft deletion was not rolled back: %v", err)
	}
	if _, _, err := store.AuthenticateNodeCredential(t.Context(), credential); err != nil {
		t.Fatalf("credential revocation was not rolled back: %v", err)
	}
}

func TestEnrollmentTokenActiveLimitIsAtomicAndReclaimable(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	makeToken := func(index int, createdAt, expiresAt time.Time) EnrollmentToken {
		plaintext := fmt.Sprintf("enrollment-%d", index)
		return EnrollmentToken{
			ID: plaintext, Digest: security.DigestEnrollmentToken(plaintext), NodeName: "node",
			CreatedAt: createdAt, ExpiresAt: expiresAt,
		}
	}
	for index := 0; index < MaxActiveEnrollmentTokens-1; index++ {
		if err := store.CreateEnrollmentToken(t.Context(), makeToken(index, now, now.Add(time.Hour))); err != nil {
			t.Fatalf("seed enrollment %d: %v", index, err)
		}
	}

	const contenders = 8
	errorsByAttempt := make(chan error, contenders)
	var group sync.WaitGroup
	for index := 0; index < contenders; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			errorsByAttempt <- store.CreateEnrollmentToken(t.Context(), makeToken(MaxActiveEnrollmentTokens+index, now, now.Add(time.Hour)))
		}(index)
	}
	group.Wait()
	close(errorsByAttempt)
	succeeded, limited := 0, 0
	for err := range errorsByAttempt {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrEnrollmentTokenLimit):
			limited++
		default:
			t.Fatalf("unexpected concurrent creation error: %v", err)
		}
	}
	if succeeded != 1 || limited != contenders-1 {
		t.Fatalf("concurrent creations: succeeded=%d limited=%d", succeeded, limited)
	}

	first := makeToken(0, now, now.Add(time.Hour))
	if _, err := store.ConsumeEnrollment(t.Context(), first.Digest, "consumed-node", "consumed-credential", security.DigestNodeCredential("credential"), now); err != nil {
		t.Fatalf("consume token at bound: %v", err)
	}
	if err := store.CreateEnrollmentToken(t.Context(), makeToken(10_000, now, now.Add(time.Hour))); err != nil {
		t.Fatalf("create after consumption: %v", err)
	}

	future := now.Add(2 * time.Hour)
	if err := store.CreateEnrollmentToken(t.Context(), makeToken(20_000, future, future.Add(time.Hour))); err != nil {
		t.Fatalf("create after expiry: %v", err)
	}
	var active int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM enrollment_tokens WHERE consumed_at IS NULL AND expires_at > ?`, unix(future)).Scan(&active); err != nil {
		t.Fatalf("count active tokens: %v", err)
	}
	if active != 1 {
		t.Fatalf("active token count after expiry=%d; want 1", active)
	}
}

func TestSingletonAdminSessionAndMFAReset(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()

	exists, err := store.AdminExists(ctx)
	if err != nil || exists {
		t.Fatalf("AdminExists() = %v, %v; want false, nil", exists, err)
	}
	admin, err := store.CreateAdmin(ctx, "owner", "argon-hash", now)
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	if _, err := store.CreateAdmin(ctx, "second", "other-hash", now); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("second CreateAdmin() error = %v; want ErrAdminExists", err)
	}

	var tokenDigest, csrfDigest [32]byte
	for index := range tokenDigest {
		tokenDigest[index] = byte(index + 1)
		csrfDigest[index] = byte(255 - index)
	}
	session := Session{
		ID: "session-1", AdminID: admin.ID, TokenDigest: tokenDigest, CSRFDigest: csrfDigest,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := store.CreateSession(ctx, session, false, admin.PasswordHash); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	gotSession, err := store.SessionByTokenDigest(ctx, tokenDigest, now.Add(time.Minute))
	if err != nil || gotSession.ID != session.ID {
		t.Fatalf("SessionByTokenDigest() = %#v, %v", gotSession, err)
	}
	if _, err := store.SessionByTokenDigest(ctx, tokenDigest, now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired SessionByTokenDigest() error = %v; want ErrNotFound", err)
	}

	cipher := []byte("encrypted-totp-secret")
	if err := store.SetPendingTOTP(ctx, admin.ID, cipher, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetPendingTOTP() error = %v", err)
	}
	otherToken := tokenDigest
	otherToken[0] ^= 0xff
	otherCSRF := csrfDigest
	otherCSRF[0] ^= 0xff
	if err := store.CreateSession(ctx, Session{
		ID: "session-2", AdminID: admin.ID, TokenDigest: otherToken, CSRFDigest: otherCSRF,
		CreatedAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour),
	}, false, admin.PasswordHash); err != nil {
		t.Fatalf("CreateSession(other) error = %v", err)
	}
	var firstCode, secondCode [32]byte
	firstCode[0] = 1
	secondCode[0] = 2
	if err := store.ConfirmTOTP(ctx, admin.ID, session.ID, [][32]byte{firstCode, secondCode}, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("ConfirmTOTP() error = %v", err)
	}
	if _, err := store.SessionByTokenDigest(ctx, otherToken, now.Add(3*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other session after TOTP confirmation error = %v; want ErrNotFound", err)
	}
	if _, err := store.SessionByTokenDigest(ctx, tokenDigest, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("kept session after TOTP confirmation error = %v", err)
	}
	if err := store.ConsumeRecoveryCode(ctx, admin.ID, firstCode, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("ConsumeRecoveryCode() error = %v", err)
	}
	if err := store.ConsumeRecoveryCode(ctx, admin.ID, firstCode, now.Add(4*time.Minute)); !errors.Is(err, ErrRecoveryCode) {
		t.Fatalf("reused recovery code error = %v; want ErrRecoveryCode", err)
	}

	if err := store.ResetMFA(ctx, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("ResetMFA() error = %v", err)
	}
	admin, err = store.AdminByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("AdminByID() error = %v", err)
	}
	if admin.TOTPConfirmed || len(admin.TOTPSecretCipher) != 0 {
		t.Fatalf("admin MFA state after reset = confirmed:%v cipher:%q", admin.TOTPConfirmed, admin.TOTPSecretCipher)
	}
	if _, err := store.SessionByTokenDigest(ctx, tokenDigest, now.Add(6*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session after MFA reset error = %v; want ErrNotFound", err)
	}
	if err := store.ConsumeRecoveryCode(ctx, admin.ID, secondCode, now.Add(6*time.Minute)); !errors.Is(err, ErrRecoveryCode) {
		t.Fatalf("recovery code after reset error = %v; want ErrRecoveryCode", err)
	}
}

func TestSessionRevocationIsScopedToOwner(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	admin, err := store.CreateAdmin(ctx, "owner", "argon-hash", now)
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	var token, csrf [32]byte
	token[0], csrf[0] = 7, 8
	if err := store.CreateSession(ctx, Session{
		ID: "session-1", AdminID: admin.ID, TokenDigest: token, CSRFDigest: csrf,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}, false, admin.PasswordHash); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := store.RevokeSession(ctx, admin.ID+1, "session-1", now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner RevokeSession() error = %v; want ErrNotFound", err)
	}
	if err := store.RevokeSession(ctx, admin.ID, "session-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if _, err := store.SessionByTokenDigest(ctx, token, now.Add(2*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked SessionByTokenDigest() error = %v; want ErrNotFound", err)
	}
}

func TestCreateSessionPrunesInactiveHistory(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	admin, err := store.CreateAdmin(ctx, "owner", "argon-hash", now)
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	makeSession := func(id string, marker byte, created, expires time.Time) Session {
		var token, csrf [32]byte
		token[0], csrf[0] = marker, marker+64
		return Session{
			ID: id, AdminID: admin.ID, TokenDigest: token, CSRFDigest: csrf,
			CreatedAt: created, ExpiresAt: expires,
		}
	}
	expired := makeSession("expired", 1, now, now.Add(time.Minute))
	revoked := makeSession("revoked", 2, now, now.Add(time.Hour))
	if err := store.CreateSession(ctx, expired, false, admin.PasswordHash); err != nil {
		t.Fatalf("create expired candidate: %v", err)
	}
	if err := store.CreateSession(ctx, revoked, false, admin.PasswordHash); err != nil {
		t.Fatalf("create revoked candidate: %v", err)
	}
	if err := store.RevokeSession(ctx, admin.ID, revoked.ID, now.Add(30*time.Second)); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	fresh := makeSession("fresh", 3, now.Add(2*time.Minute), now.Add(time.Hour))
	if err := store.CreateSession(ctx, fresh, false, admin.PasswordHash); err != nil {
		t.Fatalf("create fresh session: %v", err)
	}
	records, err := store.ListSessions(ctx, admin.ID)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(records) != 1 || records[0].ID != fresh.ID {
		t.Fatalf("sessions after pruning = %#v; want only fresh session", records)
	}
}

func TestCreateSessionBoundsActiveSessions(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	admin, err := store.CreateAdmin(ctx, "owner", "argon-hash", now)
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	for index := 0; index < maxActiveSessions+2; index++ {
		var token, csrf [32]byte
		token[0], token[1] = byte(index), 1
		csrf[0], csrf[1] = byte(index), 2
		// All rows intentionally share the database's one-second timestamp.
		// Descending IDs ensure the test catches accidental lexical tie-breaking
		// instead of insertion-order eviction.
		created := now
		id := fmt.Sprintf("session-%02d", maxActiveSessions+2-index)
		if err := store.CreateSession(ctx, Session{
			ID: id, AdminID: admin.ID,
			TokenDigest: token, CSRFDigest: csrf, CreatedAt: created, ExpiresAt: created.Add(time.Hour),
		}, false, admin.PasswordHash); err != nil {
			t.Fatalf("CreateSession(%d) error = %v", index, err)
		}
	}
	records, err := store.ListSessions(ctx, admin.ID)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(records) != maxActiveSessions {
		t.Fatalf("active session count = %d; want %d", len(records), maxActiveSessions)
	}
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		seen[record.ID] = true
	}
	oldest := fmt.Sprintf("session-%02d", maxActiveSessions+2)
	secondOldest := fmt.Sprintf("session-%02d", maxActiveSessions+1)
	newest := "session-01"
	if seen[oldest] || seen[secondOldest] || !seen[newest] {
		t.Fatalf("session eviction kept wrong records: %#v", seen)
	}
}

func TestConfirmTOTPRequiresActiveKeptSessionAndRollsBack(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	admin, err := store.CreateAdmin(ctx, "owner", "argon-hash", now)
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	if err := store.SetPendingTOTP(ctx, admin.ID, []byte("encrypted-secret"), now); err != nil {
		t.Fatalf("SetPendingTOTP() error = %v", err)
	}
	var token, csrf, recovery [32]byte
	token[0], csrf[0], recovery[0] = 1, 2, 3
	if err := store.CreateSession(ctx, Session{
		ID: "other-session", AdminID: admin.ID, TokenDigest: token, CSRFDigest: csrf,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}, false, admin.PasswordHash); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := store.ConfirmTOTP(ctx, admin.ID, "missing-current-session", [][32]byte{recovery}, now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmTOTP() error = %v; want ErrNotFound", err)
	}
	admin, err = store.AdminByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("AdminByID() error = %v", err)
	}
	if admin.TOTPConfirmed {
		t.Fatal("failed TOTP confirmation committed the administrator state")
	}
	if _, err := store.SessionByTokenDigest(ctx, token, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("failed TOTP confirmation revoked another session: %v", err)
	}
	if err := store.ConsumeRecoveryCode(ctx, admin.ID, recovery, now.Add(2*time.Minute)); !errors.Is(err, ErrRecoveryCode) {
		t.Fatalf("failed TOTP confirmation stored recovery code: %v", err)
	}
}

func TestCreateSessionRejectsStaleMFAState(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	admin, err := store.CreateAdmin(ctx, "owner", "argon-hash", now)
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	var keepToken, keepCSRF [32]byte
	keepToken[0], keepCSRF[0] = 1, 2
	if err := store.CreateSession(ctx, Session{
		ID: "keep", AdminID: admin.ID, TokenDigest: keepToken, CSRFDigest: keepCSRF,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}, false, admin.PasswordHash); err != nil {
		t.Fatalf("CreateSession(keep) error = %v", err)
	}
	if err := store.SetPendingTOTP(ctx, admin.ID, []byte("encrypted-secret"), now); err != nil {
		t.Fatalf("SetPendingTOTP() error = %v", err)
	}
	var recovery [32]byte
	recovery[0] = 3
	if err := store.ConfirmTOTP(ctx, admin.ID, "keep", [][32]byte{recovery}, now.Add(time.Minute)); err != nil {
		t.Fatalf("ConfirmTOTP() error = %v", err)
	}

	var staleToken, staleCSRF [32]byte
	staleToken[0], staleCSRF[0] = 4, 5
	stale := Session{
		ID: "stale", AdminID: admin.ID, TokenDigest: staleToken, CSRFDigest: staleCSRF,
		CreatedAt: now.Add(2 * time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := store.CreateSession(ctx, stale, false, admin.PasswordHash); !errors.Is(err, ErrAdminStateChanged) {
		t.Fatalf("CreateSession(stale MFA state) error = %v; want ErrAdminStateChanged", err)
	}
	if _, err := store.SessionByTokenDigest(ctx, staleToken, now.Add(2*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale-state session lookup error = %v; want ErrNotFound", err)
	}
	if err := store.CreateSession(ctx, stale, true, admin.PasswordHash); err != nil {
		t.Fatalf("CreateSession(current MFA state) error = %v", err)
	}
}
