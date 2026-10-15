package control

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
)

func autoJoinTask(id string) ProbeTask {
	return ProbeTask{ID: id, Name: id, Host: "localhost", Port: 443, IntervalSeconds: 60, AutoJoin: true}
}

func TestProbeAutoJoinQuotaAndAtomicUpdate(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	seedProbeNode(t, s, "old", now)
	for i := 0; i < protocol.MaxProbeTasks; i++ {
		if _, err := s.CreateProbeTask(t.Context(), autoJoinTask(fmt.Sprintf("auto-%d", i)), nil, now); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := s.ProbeSnapshot(t.Context(), "old")
	if _, err := s.CreateProbeTask(t.Context(), autoJoinTask("overflow"), []string{"old"}, now); !errors.Is(err, ErrProbeAutoJoinLimit) {
		t.Fatalf("create overflow=%v", err)
	}
	if _, err := s.ProbeTaskByID(t.Context(), "overflow"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("definition survived=%v", err)
	}
	manual := autoJoinTask("manual")
	manual.AutoJoin = false
	if _, err := s.CreateProbeTask(t.Context(), manual, []string{"old"}, now); err != nil {
		t.Fatal(err)
	}
	before, _ = s.ProbeSnapshot(t.Context(), "old")
	manual.AutoJoin = true
	manual.Name = "must rollback"
	if _, err := s.UpdateProbeTask(t.Context(), manual, nil, now); !errors.Is(err, ErrProbeAutoJoinLimit) {
		t.Fatalf("update overflow=%v", err)
	}
	after, _ := s.ProbeSnapshot(t.Context(), "old")
	stored, err := s.ProbeTaskByID(t.Context(), manual.ID)
	if err != nil || stored.AutoJoin || stored.Name != "manual" || len(stored.NodeIDs) != 1 || before.Version != after.Version {
		t.Fatalf("partial update: %+v %+v %v", stored, after, err)
	}
	enabled, err := s.ProbeTaskByID(t.Context(), "auto-0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProbeTask(t.Context(), enabled, nil, now); err != nil {
		t.Fatalf("unchanged at quota=%v", err)
	}
	enabled.AutoJoin = false
	if _, err := s.UpdateProbeTask(t.Context(), enabled, nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProbeTask(t.Context(), manual, []string{"old"}, now); err != nil {
		t.Fatalf("released quota=%v", err)
	}
}

func TestEnrollmentAutoJoinCommitDisableAndRollback(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	seedProbeNode(t, s, "existing", now)
	for _, id := range []string{"a", "b"} {
		if _, err := s.CreateProbeTask(t.Context(), autoJoinTask(id), nil, now); err != nil {
			t.Fatal(err)
		}
	}
	enroll := func(id string) error {
		t.Helper()
		token := EnrollmentToken{ID: "t-" + id, Digest: security.DigestEnrollmentToken(id), NodeName: id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := s.CreateEnrollmentToken(t.Context(), token); err != nil {
			t.Fatal(err)
		}
		_, err := s.ConsumeEnrollment(t.Context(), token.Digest, id, "c-"+id, security.DigestNodeCredential(id), now)
		return err
	}
	before, _ := s.ProbeSnapshot(t.Context(), "existing")
	if len(before.Tasks) != 0 {
		t.Fatal("auto join retroactively assigned")
	}
	if err := enroll("first"); err != nil {
		t.Fatal(err)
	}
	first, err := s.ProbeSnapshot(t.Context(), "first")
	if err != nil || len(first.Tasks) != 2 || first.Version != before.Version+1 {
		t.Fatalf("snapshot=%+v err=%v", first, err)
	}
	for _, id := range []string{"a", "b"} {
		task, err := s.ProbeTaskByID(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		task.AutoJoin = false
		if _, err := s.UpdateProbeTask(t.Context(), task, task.NodeIDs, now); err != nil {
			t.Fatal(err)
		}
	}
	before, _ = s.ProbeSnapshot(t.Context(), "first")
	if err := enroll("second"); err != nil {
		t.Fatal(err)
	}
	second, err := s.ProbeSnapshot(t.Context(), "second")
	first, _ = s.ProbeSnapshot(t.Context(), "first")
	if err != nil || len(second.Tasks) != 0 || second.Version != before.Version || len(first.Tasks) != 2 {
		t.Fatalf("disabled: second=%+v first=%+v err=%v", second, first, err)
	}
	task, _ := s.ProbeTaskByID(t.Context(), "a")
	task.AutoJoin = true
	if _, err := s.UpdateProbeTask(t.Context(), task, task.NodeIDs, now); err != nil {
		t.Fatal(err)
	}
	// Force version advancement failure after all enrollment writes; all must roll back.
	if _, err := s.db.Exec(`UPDATE probe_config_state SET version=?`, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if err := enroll("rollback"); err == nil {
		t.Fatal("expected version failure")
	}
	var nodes, credentials, assignments int
	var consumed sql.NullInt64
	if err := s.db.QueryRow(`SELECT count(*) FROM nodes WHERE id='rollback'`).Scan(&nodes); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM node_credentials WHERE node_id='rollback'`).Scan(&credentials); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM probe_assignments WHERE node_id='rollback'`).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT consumed_at FROM enrollment_tokens WHERE id='t-rollback'`).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if nodes != 0 || credentials != 0 || assignments != 0 || consumed.Valid {
		t.Fatalf("partial enrollment %d %d %d %+v", nodes, credentials, assignments, consumed)
	}
	if _, err := s.ConsumeEnrollment(t.Context(), security.DigestEnrollmentToken("unknown"), "invalid", "ci", security.DigestNodeCredential("invalid"), now); !errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatal(err)
	}
}

func TestProbeAutoJoinMigrationAllPreviousSchemas(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for previous := 1; previous < 10; previous++ {
		t.Run(fmt.Sprintf("v%d", previous), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "control.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at INTEGER NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			for _, m := range migrations[:previous] {
				if _, err := db.Exec(m.sql); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?,1)`, m.version, m.name); err != nil {
					t.Fatal(err)
				}
			}
			if previous >= 5 {
				if _, err := db.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES('old','old',1,1); INSERT INTO probe_tasks(id,name,host,port,interval_seconds,created_at,updated_at) VALUES('old','old','localhost',443,60,1,1); INSERT INTO probe_assignments VALUES('old','old',1)`); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			s, err := Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if previous >= 5 {
				task, err := s.ProbeTaskByID(t.Context(), "old")
				if err != nil || task.AutoJoin || len(task.NodeIDs) != 1 {
					t.Fatalf("migrated=%+v %v", task, err)
				}
			}
			backups, err := filepath.Glob(fmt.Sprintf("%s.backup-v%d-*", path, previous))
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups=%v %v", backups, err)
			}
			info, err := os.Stat(backups[0])
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("mode=%v %v", info, err)
			}
			backup, err := sql.Open("sqlite", backups[0])
			if err != nil {
				t.Fatal(err)
			}
			defer backup.Close()
			var columns, version int
			if err := backup.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('probe_tasks') WHERE name='auto_join'`).Scan(&columns); err != nil || columns != 0 {
				t.Fatalf("backup mutated=%d %v", columns, err)
			}
			if err := backup.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != previous {
				t.Fatalf("backup version=%d %v", version, err)
			}
		})
	}
}

func TestEnrollmentAutoJoinAtQuotaAndAssignmentFailure(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	for i := 0; i < protocol.MaxProbeTasks; i++ {
		if _, err := s.CreateProbeTask(t.Context(), autoJoinTask(fmt.Sprintf("quota-%d", i)), nil, now); err != nil {
			t.Fatal(err)
		}
	}
	token := EnrollmentToken{ID: "token", Digest: security.DigestEnrollmentToken("token"), NodeName: "node", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_auto_assignment BEFORE INSERT ON probe_assignments BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeEnrollment(t.Context(), token.Digest, "node", "credential", security.DigestNodeCredential("credential"), now); err == nil {
		t.Fatal("expected assignment failure")
	}
	var count int
	var consumed sql.NullInt64
	if err := s.db.QueryRow(`SELECT count(*) FROM nodes`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("nodes=%d %v", count, err)
	}
	if err := s.db.QueryRow(`SELECT consumed_at FROM enrollment_tokens WHERE id='token'`).Scan(&consumed); err != nil || consumed.Valid {
		t.Fatalf("token=%+v %v", consumed, err)
	}
	before, err := s.ProbeSnapshot(t.Context(), "node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_auto_assignment`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeEnrollment(t.Context(), token.Digest, "node", "credential", security.DigestNodeCredential("credential"), now); err != nil {
		t.Fatal(err)
	}
	after, err := s.ProbeSnapshot(t.Context(), "node")
	if err != nil || len(after.Tasks) != protocol.MaxProbeTasks || after.Version != before.Version+1 {
		t.Fatalf("snapshot=%+v %v", after, err)
	}
	if _, err := s.ConsumeEnrollment(t.Context(), token.Digest, "rejected", "rejected", security.DigestNodeCredential("rejected"), now); !errors.Is(err, ErrEnrollmentConsumed) {
		t.Fatal(err)
	}
	rejected, err := s.ProbeSnapshot(t.Context(), "rejected")
	if err != nil || len(rejected.Tasks) != 0 || rejected.Version != after.Version {
		t.Fatalf("rejected=%+v %v", rejected, err)
	}
}

func TestProbeAutoJoinConcurrentAdmission(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	for i := 0; i < protocol.MaxProbeTasks-1; i++ {
		if _, err := s.CreateProbeTask(t.Context(), autoJoinTask(fmt.Sprintf("seed-%d", i)), nil, now); err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			_, err := s.CreateProbeTask(t.Context(), autoJoinTask(fmt.Sprintf("concurrent-%d", i)), nil, now)
			results <- err
		}(i)
	}
	accepted, rejected := 0, 0
	for i := 0; i < 8; i++ {
		err := <-results
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrProbeAutoJoinLimit):
			rejected++
		default:
			t.Fatalf("concurrent error=%v", err)
		}
	}
	if accepted != 1 || rejected != 7 {
		t.Fatalf("accepted=%d rejected=%d", accepted, rejected)
	}
}
