package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	telemetrymigrations "github.com/senhao-xu/vps-status/migrations/telemetry"
	_ "modernc.org/sqlite"
)

// Store owns a serialized write connection and a separate read-only pool.
type Store struct {
	writer *sql.DB
	reader *sql.DB
	path   string
}

// Open creates or opens telemetry.db, applies its independent migrations, and
// selects incremental auto-vacuum before the first table is created.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("telemetry database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create telemetry data directory: %w", err)
	}
	existed := regularNonempty(path)
	db, err := openWriter(ctx, path)
	if err != nil {
		return nil, err
	}
	current, err := telemetrySchemaVersion(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read telemetry schema version: %w", err)
	}
	migrations, err := telemetryMigrations()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	latest := 0
	if len(migrations) != 0 {
		latest = migrations[len(migrations)-1].version
	}
	if current > latest {
		_ = db.Close()
		return nil, fmt.Errorf("telemetry database schema version %d is newer than supported version %d", current, latest)
	}
	if current < latest && existed {
		if err := checkpointDB(ctx, db, "TRUNCATE"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("checkpoint telemetry database before backup: %w", err)
		}
		if err := db.Close(); err != nil {
			return nil, fmt.Errorf("close telemetry database before backup: %w", err)
		}
		if _, err := backupTelemetry(path, current); err != nil {
			return nil, err
		}
		db, err = openWriter(ctx, path)
		if err != nil {
			return nil, err
		}
	}
	if current == 0 {
		if _, err := db.ExecContext(ctx, "PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure telemetry auto-vacuum: %w", err)
		}
		// SQLite persists the auto-vacuum pointer-map layout only after a
		// rebuild; doing it while the schema is empty is cheap and deterministic.
		if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize telemetry auto-vacuum layout: %w", err)
		}
	}
	if err := applyTelemetryMigrations(ctx, db, current, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("restrict telemetry database permissions: %w", err)
	}
	reader, err := openReader(ctx, path)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{writer: db, reader: reader, path: path}, nil
}

func openWriter(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open telemetry database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=2000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA wal_autocheckpoint=256",
		"PRAGMA journal_size_limit=4194304",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure telemetry database: %w", err)
		}
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping telemetry database: %w", err)
	}
	return db, nil
}

func openReader(ctx context.Context, path string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout%282000%29"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open telemetry read pool: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping telemetry read pool: %w", err)
	}
	return db, nil
}

// Close releases both independent connection pools.
func (s *Store) Close() error {
	var errs []error
	if s.reader != nil {
		errs = append(errs, s.reader.Close())
	}
	if s.writer != nil {
		errs = append(errs, s.writer.Close())
	}
	return errors.Join(errs...)
}

// Path returns the telemetry database path for local maintenance inspection.
func (s *Store) Path() string { return s.path }

type telemetryMigration struct {
	version int
	name    string
	sql     string
}

func telemetryMigrations() ([]telemetryMigration, error) {
	entries, err := fs.ReadDir(telemetrymigrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("list telemetry migrations: %w", err)
	}
	result := make([]telemetryMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		version, parseErr := strconv.Atoi(prefix)
		if !ok || parseErr != nil || version < 1 {
			return nil, fmt.Errorf("invalid telemetry migration name %q", entry.Name())
		}
		body, err := telemetrymigrations.Files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read telemetry migration %q: %w", entry.Name(), err)
		}
		result = append(result, telemetryMigration{version: version, name: entry.Name(), sql: string(body)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for i := range result {
		if result[i].version != i+1 {
			return nil, fmt.Errorf("telemetry migration versions must be contiguous at %q", result[i].name)
		}
	}
	return result, nil
}

func telemetrySchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='schema_migrations')`).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, nil
	}
	var version int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func applyTelemetryMigrations(ctx context.Context, db *sql.DB, current int, migrations []telemetryMigration) error {
	for _, item := range migrations {
		if item.version <= current {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin telemetry migration %q: %w", item.name, err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("initialize telemetry migration metadata: %w", err)
		}
		if _, err := tx.ExecContext(ctx, item.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply telemetry migration %q: %w", item.name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)", item.version, item.name, time.Now().UTC().Unix()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record telemetry migration %q: %w", item.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit telemetry migration %q: %w", item.name, err)
		}
	}
	return nil
}

func checkpointDB(ctx context.Context, db *sql.DB, mode string) error {
	if mode != "PASSIVE" && mode != "TRUNCATE" {
		return errors.New("invalid checkpoint mode")
	}
	var busy, logFrames, checkpointed int
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint("+mode+")").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return fmt.Errorf("database is busy (%d of %d frames checkpointed)", checkpointed, logFrames)
	}
	return nil
}

func regularNonempty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func backupTelemetry(path string, version int) (string, error) {
	name := fmt.Sprintf("%s.backup-v%d-%s", path, version, time.Now().UTC().Format("20060102T150405.000000000Z"))
	source, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open telemetry database for backup: %w", err)
	}
	defer source.Close()
	destination, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create telemetry database backup: %w", err)
	}
	ok := false
	defer func() {
		_ = destination.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := io.Copy(destination, source); err != nil {
		return "", fmt.Errorf("copy telemetry database backup: %w", err)
	}
	if err := destination.Sync(); err != nil {
		return "", fmt.Errorf("sync telemetry database backup: %w", err)
	}
	if err := destination.Close(); err != nil {
		return "", fmt.Errorf("close telemetry database backup: %w", err)
	}
	ok = true
	return name, nil
}
