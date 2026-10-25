package control

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

	controlmigrations "github.com/senhao-xu/vps-status/migrations/control"
	_ "modernc.org/sqlite"
)

var (
	// ErrAdminExists is returned when the singleton administrator already exists.
	ErrAdminExists = errors.New("administrator already exists")
	// ErrNotFound is returned when an administrator or session does not exist.
	ErrNotFound = errors.New("not found")
	// ErrRecoveryCode is returned when a recovery code is invalid or consumed.
	ErrRecoveryCode = errors.New("invalid recovery code")
	// ErrAdminStateChanged is returned when an authentication operation uses
	// administrator or session state that is no longer current.
	ErrAdminStateChanged = errors.New("administrator authentication state changed")
	// ErrEnrollmentInvalid is returned for an unknown enrollment token.
	ErrEnrollmentInvalid = errors.New("invalid enrollment token")
	// ErrEnrollmentExpired is returned for an expired enrollment token.
	ErrEnrollmentExpired = errors.New("enrollment token expired")
	// ErrEnrollmentConsumed is returned for an already consumed token.
	ErrEnrollmentConsumed = errors.New("enrollment token already consumed")
	// ErrEnrollmentTokenLimit is returned when the active enrollment-token
	// bound has been reached.
	ErrEnrollmentTokenLimit = errors.New("active enrollment token limit reached")
	// ErrCredentialRevoked is returned for a revoked node credential.
	ErrCredentialRevoked = errors.New("node credential revoked")
	// ErrProbeTaskLimit is returned when an assignment would give one node a
	// 65th active TCP probe task.
	ErrProbeTaskLimit = errors.New("node probe task limit reached")
	// ErrInvalidProbeConfiguration is returned for malformed task or assignment changes.
	ErrInvalidProbeConfiguration = errors.New("invalid probe task configuration")
	// ErrProbeAutoJoinLimit bounds tasks automatically assigned to future nodes.
	ErrProbeAutoJoinLimit = errors.New("auto-join probe task limit reached")
	// ErrProbeAssignmentExists is returned for a duplicate assignment.
	ErrProbeAssignmentExists = errors.New("probe assignment already exists")
	// ErrNotificationChannelLimit is returned when the bounded channel set is full.
	ErrNotificationChannelLimit = errors.New("notification channel limit reached")
	// ErrAlertRuleLimit is returned when the bounded alert-rule set is full.
	ErrAlertRuleLimit = errors.New("alert rule limit reached")
	// ErrAlertStateLimit is returned before current-state persistence could grow
	// beyond its hard control-plane bound.
	ErrAlertStateLimit = errors.New("alert state limit reached")
	// ErrDeliveryLimit is returned before the bounded notification outbox is full.
	ErrDeliveryLimit = errors.New("notification delivery limit reached")
	// ErrInvalidAlertConfiguration is returned for an invalid bounded rule or
	// channel-assignment shape before SQL is executed.
	ErrInvalidAlertConfiguration = errors.New("invalid alert configuration")
	// ErrInvalidPublicConfiguration is returned when a public-page update is
	// malformed or references a node that is not active.
	ErrInvalidPublicConfiguration = errors.New("invalid public page configuration")
	// ErrInvalidNodeConfiguration is returned for invalid display metadata.
	ErrInvalidNodeConfiguration = errors.New("invalid node configuration")
	// ErrHomepageNodeLimit bounds the homepage display membership.
	ErrHomepageNodeLimit = errors.New("homepage node limit reached")
)

// MaxActiveEnrollmentTokens bounds unconsumed, unexpired enrollment state in
// control.db even if an administrator repeatedly creates tokens.
const (
	MaxActiveEnrollmentTokens = 256
	MaxNotificationChannels   = 32
	MaxAlertRules             = 256
	MaxChannelsPerAlertRule   = 16
	MaxAlertStates            = 65536
	MaxNotificationDeliveries = 4096
	MaxPublicNodes            = 1000
)

// Store is the explicit-query control.db access boundary.
type Store struct {
	db   *sql.DB
	path string
}

// Admin is the internal administrator record. Secret fields must never be
// serialized directly by an HTTP handler.
type Admin struct {
	ID               int64
	Username         string
	PasswordHash     string
	TOTPSecretCipher []byte
	TOTPConfirmed    bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Session is the internal server-side session record.
type Session struct {
	ID          string
	AdminID     int64
	TokenDigest [32]byte
	CSRFDigest  [32]byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

// Node is low-growth node identity and display metadata.
type Node struct {
	ID              string
	Name            string
	Group           string
	Hidden          bool
	HomepageVisible bool
	Facts           NodeFacts
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// NodeFacts are low-frequency host identity fields, not live telemetry.
type NodeFacts struct {
	Hostname       string
	OS             string
	Kernel         string
	Architecture   string
	Virtualization string
}

// NodeCredential is a credential record. Only its digest is persisted.
type NodeCredential struct {
	ID        string
	NodeID    string
	Digest    [32]byte
	CreatedAt time.Time
	RevokedAt *time.Time
}

// EnrollmentToken is a one-time enrollment record. Only its digest is persisted.
type EnrollmentToken struct {
	ID             string
	Digest         [32]byte
	NodeName       string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
	ConsumedNodeID string
}

// TelemetryCleanupJob is low-growth cross-database retry metadata. It never
// contains telemetry values, counters, samples, or host facts.
type TelemetryCleanupJob struct {
	NodeID        string
	CreatedAt     time.Time
	Attempts      uint64
	LastAttemptAt *time.Time
}

// ProbeTask is low-growth TCP probe configuration. It never contains results.
type ProbeTask struct {
	ID              string
	Name            string
	Host            string
	Port            uint16
	IntervalSeconds uint32
	AutoJoin        bool
	NodeIDs         []string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ProbeSnapshot is one node's versioned complete configuration projection.
type ProbeSnapshot struct {
	Version uint64
	Tasks   []ProbeTask
}

// ProbeCleanupJob is low-growth task-ID-only cross-database retry metadata.
type ProbeCleanupJob struct {
	TaskID        string
	CreatedAt     time.Time
	Attempts      uint64
	LastAttemptAt *time.Time
}

// NotificationChannel is low-growth configuration. Ciphertext fields are
// internal and must never be serialized by the API.
type NotificationChannel struct {
	ID                   string
	Name                 string
	Kind                 string
	Enabled              bool
	WebhookURLCipher     []byte
	WebhookSecretCipher  []byte
	TelegramTokenCipher  []byte
	TelegramChatIDCipher []byte
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// AlertRule is one bounded live-threshold rule and its channel assignments.
type AlertRule struct {
	ID                      string
	Name                    string
	ScopeKind               string
	ScopeValue              string
	Metric                  string
	Threshold               float64
	DurationSeconds         uint32
	RecoveryHysteresis      float64
	Enabled                 bool
	ReminderIntervalSeconds uint32
	ReminderMaxCount        uint8
	ChannelIDs              []string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// AlertState is the durable, low-growth state-machine cursor for one
// rule/node pair. Normal rows retain the generation needed for event-ID
// stability across later firing cycles.
type AlertState struct {
	RuleID         string
	NodeID         string
	Status         string
	Generation     uint64
	PendingSince   *time.Time
	FiringSince    *time.Time
	ResolvedAt     *time.Time
	LastReminderAt *time.Time
	ReminderCount  uint8
	UpdatedAt      time.Time
}

// NotificationDelivery is one bounded transition/test outbox record.
type NotificationDelivery struct {
	ID            string
	EventID       string
	ChannelID     string
	RuleID        string
	NodeID        string
	EventKind     string
	EventPayload  []byte
	Status        string
	Attempts      uint8
	NextAttemptAt time.Time
	LastErrorCode string
	CreatedAt     time.Time
	CompletedAt   *time.Time
}

// PublicPage is the low-growth presentation configuration and explicit node
// publication allowlist. It never contains host facts or telemetry.
type PublicPage struct {
	Enabled     bool
	Title       string
	Description string
	NodeIDs     []string
	UpdatedAt   time.Time
}

// Open creates or opens a control database, applies its PRAGMAs and monotonic
// migrations, and restricts the database file to the current OS user.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("control database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create control data directory: %w", err)
	}

	existed := fileExists(path)
	db, err := openDB(ctx, path)
	if err != nil {
		return nil, err
	}

	current, err := schemaVersion(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read control schema version: %w", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	latest := 0
	if len(migrations) > 0 {
		latest = migrations[len(migrations)-1].version
	}
	if current > latest {
		_ = db.Close()
		return nil, fmt.Errorf("control database schema version %d is newer than supported version %d", current, latest)
	}
	if current < latest && existed {
		var busy, logFrames, checkpointedFrames int
		if checkpointErr := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointedFrames); checkpointErr != nil {
			_ = db.Close()
			return nil, fmt.Errorf("checkpoint control database before backup: %w", checkpointErr)
		}
		if busy != 0 {
			_ = db.Close()
			return nil, fmt.Errorf("checkpoint control database before backup: database is busy (%d of %d frames checkpointed)", checkpointedFrames, logFrames)
		}
		if err := db.Close(); err != nil {
			return nil, fmt.Errorf("close control database before backup: %w", err)
		}
		if _, err := backup(path, current); err != nil {
			return nil, err
		}
		db, err = openDB(ctx, path)
		if err != nil {
			return nil, err
		}
	}

	if err := applyMigrations(ctx, db, current, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("restrict control database permissions: %w", err)
	}

	return &Store{db: db, path: path}, nil
}

func openDB(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open control database: %w", err)
	}
	// PRAGMAs are connection-local. A single connection makes their application
	// deterministic and is sufficient for the low-write single-admin store.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA wal_autocheckpoint=256",
		"PRAGMA journal_size_limit=4194304",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure control database: %w", err)
		}
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping control database: %w", err)
	}
	return db, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the database path for local administrative operations.
func (s *Store) Path() string { return s.path }

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(controlmigrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("list control migrations: %w", err)
	}
	var result []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("invalid control migration name %q", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid control migration version %q", prefix)
		}
		body, err := controlmigrations.Files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read control migration %q: %w", entry.Name(), err)
		}
		result = append(result, migration{version: version, name: entry.Name(), sql: string(body)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for i := range result {
		if result[i].version != i+1 {
			return nil, fmt.Errorf("control migration versions must be contiguous at %q", result[i].name)
		}
	}
	return result, nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var exists int
	if err := db.QueryRowContext(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM sqlite_master
            WHERE type = 'table' AND name = 'schema_migrations'
        )`).Scan(&exists); err != nil {
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

func applyMigrations(ctx context.Context, db *sql.DB, current int, migrations []migration) error {
	for _, item := range migrations {
		if item.version <= current {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin control migration %q: %w", item.name, err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
            version INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            applied_at INTEGER NOT NULL
        )`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("initialize control migration metadata: %w", err)
		}
		if _, err := tx.ExecContext(ctx, item.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply control migration %q: %w", item.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)",
			item.version, item.name, time.Now().UTC().Unix(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record control migration %q: %w", item.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit control migration %q: %w", item.name, err)
		}
	}
	return nil
}

func backup(path string, version int) (string, error) {
	name := fmt.Sprintf("%s.backup-v%d-%s", path, version, time.Now().UTC().Format("20060102T150405.000000000Z"))
	source, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open control database for backup: %w", err)
	}
	defer source.Close()
	destination, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create control database backup: %w", err)
	}
	ok := false
	defer func() {
		_ = destination.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := io.Copy(destination, source); err != nil {
		return "", fmt.Errorf("copy control database backup: %w", err)
	}
	if err := destination.Sync(); err != nil {
		return "", fmt.Errorf("sync control database backup: %w", err)
	}
	if err := destination.Close(); err != nil {
		return "", fmt.Errorf("close control database backup: %w", err)
	}
	ok = true
	return name, nil
}
