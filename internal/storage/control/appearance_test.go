package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAppearancePersistenceAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.AppearanceConfig(t.Context())
	if err != nil || initial.DefaultTheme != "emerald" {
		t.Fatalf("initial appearance=%+v err=%v", initial, err)
	}
	if err := store.UpdateAppearance(t.Context(), Appearance{DefaultTheme: "classic"}); err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"", "Emerald", "custom", " classic "} {
		if err := store.UpdateAppearance(t.Context(), Appearance{DefaultTheme: theme}); !errors.Is(err, ErrInvalidAppearance) {
			t.Fatalf("theme %q err=%v", theme, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	appearance, err := reopened.AppearanceConfig(t.Context())
	if err != nil || appearance.DefaultTheme != "classic" {
		t.Fatalf("reopened appearance=%+v err=%v", appearance, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	defer cancel()
	if _, err := reopened.AppearanceConfig(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read err=%v", err)
	}
}

func TestAppearanceMigrationBacksUpEveryPreviousSchema(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for previous := 1; previous < 8; previous++ {
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
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			appearance, err := store.AppearanceConfig(t.Context())
			if err != nil || appearance.DefaultTheme != "emerald" {
				t.Fatalf("migrated appearance=%+v err=%v", appearance, err)
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
				t.Fatalf("backup mode is not 0600: info=%v err=%v", info, err)
			}
			backup, err := sql.Open("sqlite", backups[0])
			if err != nil {
				t.Fatal(err)
			}
			defer backup.Close()
			var count, version int
			if err := backup.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='appearance'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("backup mutated: appearance table count=%d err=%v", count, err)
			}
			if err := backup.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != previous {
				t.Fatalf("backup version=%d err=%v", version, err)
			}
		})
	}
}
