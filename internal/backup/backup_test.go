package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
	controlmigrations "github.com/senhao-xu/vps-status/migrations/control"
)

func TestCreateVerifyAndRestoreDefaultAndTelemetryBackup(t *testing.T) {
	data := t.TempDir()
	control, err := controlstore.Open(t.Context(), filepath.Join(data, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}
	telemetryStore, err := telemetry.Open(t.Context(), filepath.Join(data, "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := telemetryStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "master.key"), make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	for _, includeTelemetry := range []bool{false, true} {
		archive := filepath.Join(t.TempDir(), "backup.tar.gz")
		manifest, err := Create(t.Context(), Options{DataDirectory: data, OutputPath: archive, IncludeTelemetry: includeTelemetry, Version: "v1.2.3", Now: func() time.Time { return now }})
		if err != nil {
			t.Fatalf("Create(include telemetry=%v): %v", includeTelemetry, err)
		}
		if manifest.IncludeTelemetry != includeTelemetry || len(manifest.Files) != 2+boolInt(includeTelemetry) {
			t.Fatalf("manifest = %#v", manifest)
		}
		verified, err := Verify(t.Context(), archive)
		if err != nil || verified.Version != "v1.2.3" {
			t.Fatalf("Verify() = %#v, %v", verified, err)
		}
		restore := filepath.Join(t.TempDir(), "data")
		if _, err := Restore(t.Context(), archive, restore); err != nil {
			t.Fatalf("Restore(): %v", err)
		}
		if _, err := os.Stat(filepath.Join(restore, "control.db")); err != nil {
			t.Fatal(err)
		}
		_, telemetryErr := os.Stat(filepath.Join(restore, "telemetry.db"))
		if includeTelemetry && telemetryErr != nil || !includeTelemetry && !os.IsNotExist(telemetryErr) {
			t.Fatalf("restored telemetry state error = %v", telemetryErr)
		}
		if _, err := Restore(context.Background(), archive, restore); err == nil {
			t.Fatal("restore unexpectedly overwrote existing data")
		}
	}
}

func TestVerifyRejectsTamperedArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.tar.gz")
	if err := os.WriteFile(path, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(t.Context(), path); err == nil {
		t.Fatal("invalid archive verified")
	}
}

func TestRestoreRefusesAnyExistingTarget(t *testing.T) {
	_, archive := createBackupFixture(t, false)
	target := filepath.Join(t.TempDir(), "existing")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), archive, target); err == nil {
		t.Fatal("Restore() accepted an existing target directory")
	}
}

func TestCreateRefusesToReplaceExistingOutput(t *testing.T) {
	data, _ := createBackupFixture(t, false)
	output := filepath.Join(t.TempDir(), "backup.tar.gz")
	want := []byte("existing backup")
	if err := os.WriteFile(output, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(t.Context(), Options{DataDirectory: data, OutputPath: output}); err == nil {
		t.Fatal("Create() replaced an existing backup")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("existing output = %q, want %q", got, want)
	}
}

func TestCreateRejectsSymlinkedDataMembers(t *testing.T) {
	for _, member := range []string{"control.db", "master.key"} {
		t.Run(member, func(t *testing.T) {
			data, _ := createBackupFixture(t, false)
			original := filepath.Join(data, member)
			external := filepath.Join(t.TempDir(), member)
			contents, err := os.ReadFile(original)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(external, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(original); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, original); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "backup.tar.gz")
			if _, err := Create(t.Context(), Options{DataDirectory: data, OutputPath: output}); err == nil {
				t.Fatalf("Create() accepted symlinked %s", member)
			}
		})
	}
}

func TestRestoreRefusesExistingSymlinkTarget(t *testing.T) {
	_, archive := createBackupFixture(t, false)
	pointedDirectory := t.TempDir()
	target := filepath.Join(t.TempDir(), "restore")
	if err := os.Symlink(pointedDirectory, target); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), archive, target); err == nil {
		t.Fatal("Restore() accepted an existing symlink target")
	}
	if _, err := os.Stat(filepath.Join(pointedDirectory, "control.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink destination was modified: %v", err)
	}
}

func TestExtractRejectsTraversalAndLinkMembers(t *testing.T) {
	tests := []struct {
		name     string
		header   tar.Header
		contents []byte
	}{
		{name: "parent traversal", header: tar.Header{Name: "../control.db", Typeflag: tar.TypeReg, Mode: 0o600, Size: 1}, contents: []byte("x")},
		{name: "nested path", header: tar.Header{Name: "nested/control.db", Typeflag: tar.TypeReg, Mode: 0o600, Size: 1}, contents: []byte("x")},
		{name: "symlink", header: tar.Header{Name: "control.db", Typeflag: tar.TypeSymlink, Linkname: "../outside", Mode: 0o600}},
		{name: "hard link", header: tar.Header{Name: "control.db", Typeflag: tar.TypeLink, Linkname: "../outside", Mode: 0o600}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "unsafe.tar.gz")
			writeTestArchive(t, archive, test.header, test.contents)
			destination := t.TempDir()
			if _, err := extract(t.Context(), archive, destination); err == nil {
				t.Fatal("extract() accepted an unsafe archive member")
			}
			entries, err := os.ReadDir(destination)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("extract() created files for unsafe member: %v", entries)
			}
		})
	}
}

func TestRenameNoReplacePreservesExistingDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(source, destination); err == nil {
		t.Fatal("renameNoReplace() replaced an existing destination")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("destination = %q, want old", got)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source disappeared after refused publish: %v", err)
	}
}

func TestVerifyExtractedRejectsUnmanifestedAllowedMember(t *testing.T) {
	_, archive := createBackupFixture(t, false)
	staging := t.TempDir()
	manifest, err := extract(t.Context(), archive, staging)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "telemetry.db"), []byte("unmanifested"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyExtracted(t.Context(), staging, manifest); err == nil {
		t.Fatal("verifyExtracted() accepted an unmanifested archive member")
	}
}

func TestVerifyRejectsFutureSchemaAndMismatchedMasterKey(t *testing.T) {
	data := t.TempDir()
	key, err := security.LoadOrCreateMasterKey(filepath.Join(data, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := controlstore.Open(t.Context(), filepath.Join(data, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	admin, err := store.CreateAdmin(t.Context(), "owner", "test-password-hash", now)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := key.Encrypt([]byte("totp-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPendingTOTP(t.Context(), admin.ID, ciphertext, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	wrongKey := filepath.Join(t.TempDir(), "master.key")
	if _, err := security.LoadOrCreateMasterKey(wrongKey); err != nil {
		t.Fatal(err)
	}
	if err := verifyMasterKeyCoupling(t.Context(), filepath.Join(data, "control.db"), wrongKey); err == nil {
		t.Fatal("verifyMasterKeyCoupling() accepted a key that cannot decrypt stored data")
	}

	db, err := sql.Open("sqlite", filepath.Join(data, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO schema_migrations(version,name,applied_at) VALUES(999,'future',1)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyDatabase(t.Context(), filepath.Join(data, "control.db"), "admins", controlmigrations.Files); err == nil {
		t.Fatal("verifyDatabase() accepted a future schema version")
	}
}

func TestVerifyDatabaseRejectsIncompleteSchemaHistory(t *testing.T) {
	data, _ := createBackupFixture(t, false)
	databasePath := filepath.Join(data, "control.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.ExecContext(t.Context(), "DELETE FROM schema_migrations WHERE version = (SELECT MAX(version)-1 FROM schema_migrations)")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted migration rows = %d, want 1", deleted)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyDatabase(t.Context(), databasePath, "admins", controlmigrations.Files); err == nil {
		t.Fatal("verifyDatabase() accepted an incomplete schema history")
	}
}

func createBackupFixture(t *testing.T, includeTelemetry bool) (string, string) {
	t.Helper()
	data := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(data, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "master.key"), make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if includeTelemetry {
		telemetryStore, err := telemetry.Open(t.Context(), filepath.Join(data, "telemetry.db"))
		if err != nil {
			t.Fatal(err)
		}
		if err := telemetryStore.Close(); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	if _, err := Create(t.Context(), Options{DataDirectory: data, OutputPath: archive, IncludeTelemetry: includeTelemetry, Version: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	return data, archive
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func writeTestArchive(t *testing.T, path string, header tar.Header, contents []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&header); err != nil {
		t.Fatal(err)
	}
	if len(contents) > 0 {
		if _, err := tarWriter.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
