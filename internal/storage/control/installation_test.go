package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallationPersistenceAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.InstallationConfig(t.Context())
	if err != nil || initial != (Installation{}) {
		t.Fatalf("initial=%+v err=%v", initial, err)
	}
	saved := Installation{ControlURL: "https://control.example/panel", AgentReleaseVersion: "v1.2.3"}
	if err := store.UpdateInstallation(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"http://control.example", "https://localhost", "https://LOCALHOST.", "https://node.localhost", "https://127.1.2.3", "https://[::1]", "https://[::1%25lo]", "https://[::ffff:127.0.0.1]", "https://0.0.0.0", "https://user:pass@control.example", "https://control.example?", "https://control.example#x", "https://control.example?q=x", "https://" + strings.Repeat("a", 2049)} {
		if err := store.UpdateInstallation(t.Context(), Installation{ControlURL: address, AgentReleaseVersion: saved.AgentReleaseVersion}); !errors.Is(err, ErrInvalidInstallation) {
			t.Fatalf("address %q accepted: %v", address, err)
		}
	}
	for _, version := range []string{"dev", "unknown", "DeV", "Unknown", "../v1.2.3", "v1;id", "v1/x", strings.Repeat("x", 65), " v1 "} {
		if err := store.UpdateInstallation(t.Context(), Installation{ControlURL: saved.ControlURL, AgentReleaseVersion: version}); !errors.Is(err, ErrInvalidInstallation) {
			t.Fatalf("version %q accepted: %v", version, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	value, err := store.InstallationConfig(t.Context())
	if err != nil || value != saved {
		t.Fatalf("reopened=%+v err=%v", value, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.InstallationConfig(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read=%v", err)
	}
	if err := store.UpdateInstallation(ctx, saved); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write=%v", err)
	}
	if err := store.UpdateInstallation(t.Context(), Installation{}); err != nil {
		t.Fatal(err)
	}
	value, err = store.InstallationConfig(t.Context())
	if err != nil || value != (Installation{}) {
		t.Fatalf("defaults=%+v err=%v", value, err)
	}
}

func TestInstallationMigrationBacksUpEveryPreviousSchema(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for previous := 1; previous < 9; previous++ {
		t.Run(fmt.Sprintf("v%d", previous), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "control.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			for _, migration := range migrations[:previous] {
				if _, err := db.Exec(migration.sql); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?,?,1)`, migration.version, migration.name); err != nil {
					t.Fatal(err)
				}
			}
			const username, passwordHash = "migration-owner", "fake-nonsecret-migration-digest"
			if _, err := db.Exec(`INSERT INTO admins(id,username,password_hash,created_at,updated_at) VALUES (1,?,?,1,2)`, username, passwordHash); err != nil {
				t.Fatal(err)
			}
			if previous == 8 {
				if _, err := db.Exec(`UPDATE appearance SET default_theme='classic' WHERE singleton_id=1`); err != nil {
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
			value, err := store.InstallationConfig(t.Context())
			if err != nil || value != (Installation{}) {
				t.Fatalf("migration=%+v err=%v", value, err)
			}
			admin, err := store.AdminByID(t.Context(), 1)
			if err != nil || admin.Username != username || admin.PasswordHash != passwordHash || admin.CreatedAt.Unix() != 1 || admin.UpdatedAt.Unix() != 2 {
				t.Fatalf("administrator not preserved after v%d upgrade: %+v err=%v", previous, admin, err)
			}
			if previous == 8 {
				appearance, err := store.AppearanceConfig(t.Context())
				if err != nil || appearance.DefaultTheme != "classic" {
					t.Fatalf("appearance not preserved after v8 upgrade: %+v err=%v", appearance, err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			backups, err := filepath.Glob(fmt.Sprintf("%s.backup-v%d-*", path, previous))
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups=%v err=%v", backups, err)
			}
			info, err := os.Stat(backups[0])
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("backup mode: %v %v", info, err)
			}
			backup, err := sql.Open("sqlite", backups[0])
			if err != nil {
				t.Fatal(err)
			}
			defer backup.Close()
			var count, version int
			if err := backup.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='installation'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("backup mutated count=%d err=%v", count, err)
			}
			if err := backup.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != previous {
				t.Fatalf("backup version=%d err=%v", version, err)
			}
			var backedUpUsername, backedUpHash string
			var createdAt, updatedAt int64
			if err := backup.QueryRow(`SELECT username,password_hash,created_at,updated_at FROM admins WHERE id=1`).Scan(&backedUpUsername, &backedUpHash, &createdAt, &updatedAt); err != nil || backedUpUsername != username || backedUpHash != passwordHash || createdAt != 1 || updatedAt != 2 {
				t.Fatalf("administrator not preserved in v%d backup: username=%q digest=%q created=%d updated=%d err=%v", previous, backedUpUsername, backedUpHash, createdAt, updatedAt, err)
			}
			if previous == 8 {
				var theme string
				if err := backup.QueryRow(`SELECT default_theme FROM appearance WHERE singleton_id=1`).Scan(&theme); err != nil || theme != "classic" {
					t.Fatalf("appearance not preserved in v8 backup: theme=%q err=%v", theme, err)
				}
			}
		})
	}
}

func TestRemoteControlURLRejectsNumericLoopbackAliases(t *testing.T) {
	for _, host := range []string{"127.1", "127.0.1", "2130706433", "0x7f000001", "0177.0.0.1", "0x7f.1", "127.1.", "0", "0x0", "00.0", "0.0.0"} {
		t.Run(host, func(t *testing.T) {
			if _, err := ValidateRemoteControlURL("https://" + host); !errors.Is(err, ErrInvalidInstallation) {
				t.Fatalf("numeric local alias %q accepted: %v", host, err)
			}
		})
	}
	for _, address := range []string{"https://status.example", "https://192.168.1.10:8443", "https://[2001:db8::1]", "https://127.example", "https://example.2130706433"} {
		if normalized, err := ValidateRemoteControlURL(address); err != nil || normalized != address {
			t.Fatalf("ordinary remote address %q rejected: %q %v", address, normalized, err)
		}
	}
}
