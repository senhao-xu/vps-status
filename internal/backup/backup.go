// Package backup creates and verifies portable Control data backups.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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

	"github.com/senhao-xu/vps-status/internal/security"
	controlmigrations "github.com/senhao-xu/vps-status/migrations/control"
	telemetrymigrations "github.com/senhao-xu/vps-status/migrations/telemetry"
	_ "modernc.org/sqlite"
)

const (
	manifestName      = "manifest.json"
	formatVersion     = 1
	maxManifestBytes  = 64 << 10
	maxMasterKeyBytes = 1024
	maxArchiveMember  = int64(64 << 30)
	maxArchiveBytes   = int64(64 << 30)
)

var allowedFiles = map[string]bool{
	"control.db":   true,
	"master.key":   true,
	"telemetry.db": true,
}

// Options controls one backup. Telemetry is deliberately opt-in because it
// is the high-growth, independently recoverable database.
type Options struct {
	DataDirectory    string
	OutputPath       string
	IncludeTelemetry bool
	Version          string
	Now              func() time.Time
}

// Manifest describes the archive and authenticates each contained file.
type Manifest struct {
	Format           int        `json:"format"`
	CreatedAt        time.Time  `json:"created_at"`
	Version          string     `json:"version"`
	IncludeTelemetry bool       `json:"include_telemetry"`
	Files            []FileInfo `json:"files"`
}

// FileInfo is one allowlisted archive member.
type FileInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Create writes a mode-0600 tar.gz containing an online SQLite snapshot of
// control.db, the deployment master key, and optionally telemetry.db.
func Create(ctx context.Context, options Options) (Manifest, error) {
	if options.DataDirectory == "" || options.OutputPath == "" {
		return Manifest{}, errors.New("backup data directory and output path are required")
	}
	dataRoot, err := filepath.EvalSymlinks(options.DataDirectory)
	if err != nil {
		return Manifest{}, errors.New("resolve backup data directory")
	}
	dataRoot, err = filepath.Abs(dataRoot)
	if err != nil {
		return Manifest{}, errors.New("resolve backup data directory")
	}
	dataInfo, err := os.Stat(dataRoot)
	if err != nil || !dataInfo.IsDir() {
		return Manifest{}, errors.New("backup data path is not a directory")
	}
	outputParent, err := filepath.EvalSymlinks(filepath.Dir(options.OutputPath))
	if err != nil {
		return Manifest{}, errors.New("resolve backup output directory")
	}
	outputParent, err = filepath.Abs(outputParent)
	if err != nil {
		return Manifest{}, errors.New("resolve backup output directory")
	}
	outputName := filepath.Base(options.OutputPath)
	if outputName == "." || outputName == ".." || outputName == string(filepath.Separator) {
		return Manifest{}, errors.New("backup output path must name a file")
	}
	outputPath := filepath.Join(outputParent, outputName)
	if _, err := os.Lstat(outputPath); err == nil {
		return Manifest{}, errors.New("backup output must not already exist")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, fmt.Errorf("inspect backup output: %w", err)
	}
	for name := range allowedFiles {
		dataPath := filepath.Join(dataRoot, name)
		if outputPath == dataPath {
			return Manifest{}, errors.New("backup output must not replace a Control data file")
		}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	createdAt := options.Now().UTC().Truncate(time.Second)
	staging, err := os.MkdirTemp(outputParent, ".vps-status-backup-*")
	if err != nil {
		return Manifest{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	files := []string{"control.db", "master.key"}
	if options.IncludeTelemetry {
		files = append(files, "telemetry.db")
	}
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		source := filepath.Join(dataRoot, name)
		destination := filepath.Join(staging, name)
		if strings.HasSuffix(name, ".db") {
			if err := snapshotSQLite(ctx, source, destination); err != nil {
				return Manifest{}, fmt.Errorf("snapshot %s: %w", name, err)
			}
		} else if err := copyRegular(source, destination, maxMasterKeyBytes); err != nil {
			return Manifest{}, fmt.Errorf("copy %s: %w", name, err)
		}
	}

	manifest := Manifest{Format: formatVersion, CreatedAt: createdAt, Version: options.Version, IncludeTelemetry: options.IncludeTelemetry}
	for _, name := range files {
		info, err := hashFile(filepath.Join(staging, name), name)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Files = append(manifest.Files, info)
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Name < manifest.Files[j].Name })
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, fmt.Errorf("encode backup manifest: %w", err)
	}
	manifestJSON = append(manifestJSON, '\n')
	temporary, err := os.CreateTemp(outputParent, ".vps-status-archive-*")
	if err != nil {
		return Manifest{}, fmt.Errorf("create backup archive: %w", err)
	}
	temporaryPath := temporary.Name()
	complete := false
	defer func() {
		_ = temporary.Close()
		if !complete {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return Manifest{}, fmt.Errorf("restrict backup archive: %w", err)
	}
	gzipWriter := gzip.NewWriter(temporary)
	gzipWriter.Header.ModTime = time.Time{}
	gzipWriter.Header.Name = ""
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range manifest.Files {
		if err := addTarFile(tarWriter, filepath.Join(staging, file.Name), file.Name, createdAt); err != nil {
			return Manifest{}, err
		}
	}
	if err := addTarBytes(tarWriter, manifestName, manifestJSON, createdAt); err != nil {
		return Manifest{}, err
	}
	if err := tarWriter.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close backup tar stream: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close backup gzip stream: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return Manifest{}, fmt.Errorf("sync backup archive: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close backup archive: %w", err)
	}
	if err := renameNoReplace(temporaryPath, outputPath); err != nil {
		return Manifest{}, fmt.Errorf("publish backup archive: %w", err)
	}
	if err := syncDirectory(outputParent); err != nil {
		return Manifest{}, fmt.Errorf("sync backup archive directory: %w", err)
	}
	complete = true
	return manifest, nil
}

// Verify validates archive paths, hashes, required files, master-key shape,
// and SQLite quick_check/schema markers without modifying production data.
func Verify(ctx context.Context, archivePath string) (Manifest, error) {
	staging, err := os.MkdirTemp("", "vps-status-verify-*")
	if err != nil {
		return Manifest{}, fmt.Errorf("create verification directory: %w", err)
	}
	defer os.RemoveAll(staging)
	manifest, err := extract(ctx, archivePath, staging)
	if err != nil {
		return Manifest{}, err
	}
	if err := verifyExtracted(ctx, staging, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Restore verifies an archive and atomically publishes it at a path that does
// not already exist. Operators must stop Control before invoking it.
func Restore(ctx context.Context, archivePath, targetDirectory string) (Manifest, error) {
	if targetDirectory == "" {
		return Manifest{}, errors.New("restore target directory is required")
	}
	parent := filepath.Dir(targetDirectory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("create restore parent directory: %w", err)
	}
	parent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return Manifest{}, errors.New("resolve restore parent directory")
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return Manifest{}, errors.New("resolve restore parent directory")
	}
	targetName := filepath.Base(targetDirectory)
	if targetName == "." || targetName == ".." || targetName == string(filepath.Separator) {
		return Manifest{}, errors.New("restore target path must name a new directory")
	}
	targetPath := filepath.Join(parent, targetName)
	if _, err := os.Lstat(targetPath); err == nil {
		return Manifest{}, errors.New("restore target must not already exist")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, fmt.Errorf("inspect restore target: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".vps-status-restore-*")
	if err != nil {
		return Manifest{}, fmt.Errorf("create restore staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	manifest, err := extract(ctx, archivePath, staging)
	if err != nil {
		return Manifest{}, err
	}
	if err := verifyExtracted(ctx, staging, manifest); err != nil {
		return Manifest{}, err
	}
	if err := syncDirectory(staging); err != nil {
		return Manifest{}, fmt.Errorf("sync restore staging directory: %w", err)
	}
	if err := renameNoReplace(staging, targetPath); err != nil {
		return Manifest{}, fmt.Errorf("publish restored data directory: %w", err)
	}
	staging = ""
	if err := syncDirectory(parent); err != nil {
		return Manifest{}, fmt.Errorf("sync restored data directory: %w", err)
	}
	return manifest, nil
}

func snapshotSQLite(ctx context.Context, source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("database is not a regular file")
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		return err
	}
	// VACUUM INTO is SQLite's consistent online snapshot operation and includes
	// committed WAL content without copying live sidecar files.
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", destination); err != nil {
		return err
	}
	return os.Chmod(destination, 0o600)
}

func extract(ctx context.Context, archivePath, destination string) (Manifest, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return Manifest{}, fmt.Errorf("open backup archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return Manifest{}, errors.New("backup archive is not valid gzip")
	}
	defer gzipReader.Close()
	typed := tar.NewReader(gzipReader)
	seen := make(map[string]bool)
	var manifest Manifest
	manifestSeen := false
	var extractedBytes int64
	for {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		header, err := typed.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, errors.New("backup tar stream is invalid")
		}
		name := header.Name
		if header.Typeflag != tar.TypeReg || filepath.Base(name) != name || name == "." || seen[name] {
			return Manifest{}, errors.New("backup archive contains an invalid member")
		}
		seen[name] = true
		if name == manifestName {
			if header.Size < 1 || header.Size > maxManifestBytes {
				return Manifest{}, errors.New("backup manifest has an invalid size")
			}
			decoder := json.NewDecoder(io.LimitReader(typed, maxManifestBytes))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&manifest); err != nil {
				return Manifest{}, errors.New("backup manifest is invalid")
			}
			var trailing any
			if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
				return Manifest{}, errors.New("backup manifest has trailing data")
			}
			manifestSeen = true
			continue
		}
		if !allowedFiles[name] || header.Size < 1 || header.Size > maxArchiveMember {
			return Manifest{}, errors.New("backup archive contains an unexpected member")
		}
		if extractedBytes > maxArchiveBytes-header.Size {
			return Manifest{}, errors.New("backup archive exceeds the extraction size limit")
		}
		extractedBytes += header.Size
		output, err := os.OpenFile(filepath.Join(destination, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return Manifest{}, fmt.Errorf("extract backup member: %w", err)
		}
		written, copyErr := io.CopyN(output, typed, header.Size)
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil || written != header.Size || syncErr != nil || closeErr != nil {
			return Manifest{}, errors.New("backup member is truncated")
		}
	}
	if !manifestSeen {
		return Manifest{}, errors.New("backup manifest is missing")
	}
	return manifest, nil
}

func verifyExtracted(ctx context.Context, directory string, manifest Manifest) error {
	if manifest.Format != formatVersion || manifest.CreatedAt.IsZero() || len(manifest.Files) < 2 || len(manifest.Files) > 3 {
		return errors.New("backup manifest contract is invalid")
	}
	expected := map[string]bool{"control.db": true, "master.key": true}
	if manifest.IncludeTelemetry {
		expected["telemetry.db"] = true
	}
	seen := make(map[string]bool)
	for _, file := range manifest.Files {
		if !expected[file.Name] || seen[file.Name] || file.Size < 1 || len(file.SHA256) != sha256.Size*2 {
			return errors.New("backup manifest file list is invalid")
		}
		seen[file.Name] = true
		actual, err := hashFile(filepath.Join(directory, file.Name), file.Name)
		if err != nil {
			return err
		}
		if actual.Size != file.Size || actual.SHA256 != file.SHA256 {
			return fmt.Errorf("backup member %s failed checksum verification", file.Name)
		}
	}
	if len(seen) != len(expected) {
		return errors.New("backup is missing a required member")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != len(expected) {
		return errors.New("backup contains a member that is not covered by its manifest")
	}
	for _, entry := range entries {
		if entry.IsDir() || !expected[entry.Name()] {
			return errors.New("backup contains a member that is not covered by its manifest")
		}
	}
	controlPath := filepath.Join(directory, "control.db")
	if err := verifyDatabase(ctx, controlPath, "admins", controlmigrations.Files); err != nil {
		return fmt.Errorf("control database verification failed: %w", err)
	}
	if err := verifyMasterKeyCoupling(ctx, controlPath, filepath.Join(directory, "master.key")); err != nil {
		return fmt.Errorf("control database master-key verification failed: %w", err)
	}
	if manifest.IncludeTelemetry {
		if err := verifyDatabase(ctx, filepath.Join(directory, "telemetry.db"), "host_1m", telemetrymigrations.Files); err != nil {
			return fmt.Errorf("telemetry database verification failed: %w", err)
		}
	}
	return nil
}

func verifyDatabase(ctx context.Context, path, requiredTable string, migrationFiles fs.FS) error {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=query_only%281%29&_pragma=busy_timeout%282000%29"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil || result != "ok" {
		return errors.New("SQLite quick_check did not return ok")
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", requiredTable).Scan(&count); err != nil || count != 1 {
		return fmt.Errorf("required table %s is missing", requiredTable)
	}
	latest, err := latestMigrationVersion(migrationFiles)
	if err != nil {
		return err
	}
	var migrationCount, minimum, maximum int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(MIN(version),0),COALESCE(MAX(version),0) FROM schema_migrations").Scan(&migrationCount, &minimum, &maximum); err != nil {
		return errors.New("schema migration metadata is missing")
	}
	if minimum != 1 || maximum < 1 || maximum > latest || migrationCount != maximum {
		return fmt.Errorf("schema version %d is not compatible with supported version %d", maximum, latest)
	}
	return nil
}

func latestMigrationVersion(files fs.FS) (int, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return 0, errors.New("read embedded migration metadata")
	}
	latest := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		version, parseErr := strconv.Atoi(prefix)
		if !ok || parseErr != nil || version < 1 {
			return 0, errors.New("embedded migration metadata is invalid")
		}
		if version > latest {
			latest = version
		}
	}
	if latest == 0 {
		return 0, errors.New("embedded migration metadata is empty")
	}
	return latest, nil
}

func verifyMasterKeyCoupling(ctx context.Context, databasePath, keyPath string) error {
	key, err := security.LoadOrCreateMasterKey(keyPath)
	if err != nil {
		return errors.New("master key is invalid")
	}
	dsn := "file:" + filepath.ToSlash(databasePath) + "?mode=ro&_pragma=query_only%281%29&_pragma=busy_timeout%282000%29"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return errors.New("open control database for key verification")
	}
	defer db.Close()
	queries := []string{"SELECT totp_secret_cipher FROM admins WHERE totp_secret_cipher IS NOT NULL"}
	var channelsExist int
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='notification_channels')").Scan(&channelsExist); err != nil {
		return errors.New("inspect encrypted control fields")
	}
	if channelsExist == 1 {
		queries = append(queries, `SELECT value FROM (
			SELECT webhook_url_cipher AS value FROM notification_channels UNION ALL
			SELECT webhook_secret_cipher FROM notification_channels UNION ALL
			SELECT telegram_token_cipher FROM notification_channels UNION ALL
			SELECT telegram_chat_id_cipher FROM notification_channels
		) WHERE value IS NOT NULL`)
	}
	for _, query := range queries {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return errors.New("read encrypted control fields")
		}
		for rows.Next() {
			var envelope []byte
			if err := rows.Scan(&envelope); err != nil {
				_ = rows.Close()
				return errors.New("read encrypted control field")
			}
			if len(envelope) > 0 {
				if _, err := key.Decrypt(envelope); err != nil {
					_ = rows.Close()
					return errors.New("master key does not authenticate encrypted control data")
				}
			}
		}
		if err := rows.Close(); err != nil {
			return errors.New("close encrypted control field query")
		}
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func hashFile(path, name string) (FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return FileInfo{}, fmt.Errorf("open backup member %s: %w", name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return FileInfo{}, fmt.Errorf("backup member %s is not a regular file", name)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return FileInfo{}, fmt.Errorf("hash backup member %s: %w", name, err)
	}
	return FileInfo{Name: name, Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func copyRegular(source, destination string, maximum int64) error {
	pathInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !pathInfo.Mode().IsRegular() {
		return errors.New("source is not a regular file")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum {
		return errors.New("source is not a bounded regular file")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}

func addTarFile(writer *tar.Writer, path, name string, modTime time.Time) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: modTime, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

func addTarBytes(writer *tar.Writer, name string, data []byte, modTime time.Time) error {
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: modTime, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := writer.Write(data)
	return err
}
