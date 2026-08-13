package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const (
	CoreDataset     = "core"
	ContactsDataset = "contacts"
	MessagesDataset = "messages"
	CallsDataset    = "calls"
	RuntimeDataset  = "runtime"

	storageMarkerName    = ".simplus-storage-root"
	storageMarkerContent = "simplus-storage-root-v1\n"
)

var datasetNames = []string{
	CoreDataset,
	ContactsDataset,
	MessagesDataset,
	CallsDataset,
	RuntimeDataset,
}

//go:embed migrations/*/*.sql
var migrationFiles embed.FS

var migrationMu sync.Mutex

type Set struct {
	Root     string
	Core     *sql.DB
	Contacts *sql.DB
	Messages *sql.DB
	Calls    *sql.DB
	Runtime  *sql.DB
}

type fileIdentity struct {
	device uint64
	inode  uint64
}

type trustedDirectory struct {
	path     string
	identity fileIdentity
	mode     os.FileMode
	uid      uint32
}

type schemaObject struct {
	kind  string
	name  string
	table string
	sql   string
}

func OpenSet(ctx context.Context, root string) (*Set, error) {
	canonicalRoot, err := prepareRoot(root)
	if err != nil {
		return nil, err
	}

	opened := make(map[string]*sql.DB, len(datasetNames))
	for _, name := range datasetNames {
		db, err := openDataset(ctx, canonicalRoot, name)
		if err != nil {
			for _, existing := range opened {
				_ = existing.Close()
			}
			return nil, err
		}
		opened[name] = db
	}

	return &Set{
		Root:     canonicalRoot,
		Core:     opened[CoreDataset],
		Contacts: opened[ContactsDataset],
		Messages: opened[MessagesDataset],
		Calls:    opened[CallsDataset],
		Runtime:  opened[RuntimeDataset],
	}, nil
}

func (set *Set) Close() error {
	if set == nil {
		return nil
	}
	var joined error
	for _, db := range []*sql.DB{set.Runtime, set.Calls, set.Messages, set.Contacts, set.Core} {
		if db == nil {
			continue
		}
		if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			joined = errors.Join(joined, err)
		}
		joined = errors.Join(joined, db.Close())
	}
	return joined
}

func prepareRoot(root string) (string, error) {
	if root == "" {
		return "", errors.New("sqlite root must not be empty")
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("sqlite root must be an absolute path")
	}
	absolute := filepath.Clean(root)
	if absolute == string(filepath.Separator) {
		return "", errors.New("sqlite root must not be the filesystem root")
	}

	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve sqlite root parent: %w", err)
	}
	trustedAncestors, err := validateTrustedDirectoryChain(parent)
	if err != nil {
		return "", fmt.Errorf("validate sqlite root ancestors: %w", err)
	}
	canonical := filepath.Join(parent, filepath.Base(absolute))
	created := false
	if err := os.Mkdir(canonical, 0o700); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("create sqlite root: %w", err)
	}

	info, err := secureOwnedPath(canonical, true)
	if err != nil {
		return "", fmt.Errorf("validate sqlite root: %w", err)
	}
	if info.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("sqlite root permissions must be 0700, found %04o: %s", info.Mode().Perm(), canonical)
	}
	if created {
		if err := createStorageMarker(canonical); err != nil {
			return "", err
		}
	} else if err := validateOrInitializeStorageMarker(canonical); err != nil {
		return "", err
	}
	parentAfter, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("revalidate sqlite root parent: %w", err)
	}
	if parentAfter != parent {
		return "", errors.New("sqlite root parent changed while being validated")
	}
	trustedAncestorsAfter, err := validateTrustedDirectoryChain(parentAfter)
	if err != nil {
		return "", fmt.Errorf("revalidate sqlite root ancestors: %w", err)
	}
	if !sameTrustedDirectoryChain(trustedAncestors, trustedAncestorsAfter) {
		return "", errors.New("sqlite root ancestor identity changed while being validated")
	}
	canonicalAfter, err := filepath.EvalSymlinks(canonical)
	if err != nil {
		return "", fmt.Errorf("revalidate sqlite root: %w", err)
	}
	if canonicalAfter != canonical {
		return "", errors.New("sqlite root resolved unexpectedly while being validated")
	}
	return canonical, nil
}

func createStorageMarker(root string) error {
	path := filepath.Join(root, storageMarkerName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create sqlite root marker: %w", err)
	}
	if _, err := file.WriteString(storageMarkerContent); err != nil {
		_ = file.Close()
		return fmt.Errorf("write sqlite root marker: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync sqlite root marker: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close sqlite root marker: %w", err)
	}
	if err := syncDirectory(root); err != nil {
		return fmt.Errorf("sync sqlite root directory: %w", err)
	}
	return validateStorageMarker(path)
}

func validateOrInitializeStorageMarker(root string) error {
	path := filepath.Join(root, storageMarkerName)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return fmt.Errorf("inspect unmarked sqlite root: %w", readErr)
		}
		if len(entries) != 0 {
			return fmt.Errorf("refusing non-empty unmarked sqlite root: %s", root)
		}
		return createStorageMarker(root)
	} else if err != nil {
		return fmt.Errorf("inspect sqlite root marker: %w", err)
	}
	return validateStorageMarker(path)
}

func validateStorageMarker(path string) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open sqlite root marker without following links: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return errors.New("open sqlite root marker: invalid file descriptor")
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat sqlite root marker: %w", err)
	}
	if _, err := validateOwnedRegularFile(path, info, 0o600); err != nil {
		return fmt.Errorf("validate sqlite root marker: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(len(storageMarkerContent)+1)))
	if err != nil {
		return fmt.Errorf("read sqlite root marker: %w", err)
	}
	if string(content) != storageMarkerContent {
		return errors.New("sqlite root marker content mismatch")
	}
	return nil
}

func openDataset(ctx context.Context, root, name string) (*sql.DB, error) {
	path := filepath.Join(root, name+".sqlite3")
	identity, created, err := prepareDatabaseFile(path, name)
	if err != nil {
		return nil, err
	}
	if !created {
		if err := validateDatabaseArtifacts(path); err != nil {
			return nil, err
		}
		if err := preflightExistingDataset(ctx, path, name); err != nil {
			return nil, err
		}
		if err := requireSamePathIdentity(path, identity); err != nil {
			return nil, fmt.Errorf("%s database changed during preflight: %w", name, err)
		}
	}

	query := make(url.Values)
	query.Set("mode", "rw")
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", name, err)
	}
	db.SetMaxOpenConns(1)
	closeWithError := func(openErr error) (*sql.DB, error) {
		_ = db.Close()
		return nil, openErr
	}

	if err := db.PingContext(ctx); err != nil {
		return closeWithError(fmt.Errorf("ping %s database: %w", name, err))
	}
	if err := requireSamePathIdentity(path, identity); err != nil {
		return closeWithError(fmt.Errorf("%s database changed before write access: %w", name, err))
	}
	if err := configureDatabase(ctx, db, name); err != nil {
		return closeWithError(err)
	}
	if err := migrate(ctx, db, name); err != nil {
		return closeWithError(err)
	}
	if err := verifyDatasetIdentity(ctx, db, name); err != nil {
		return closeWithError(err)
	}
	if err := verifyCurrentSchema(ctx, db, name); err != nil {
		return closeWithError(err)
	}
	if err := verifyDatabaseIntegrity(ctx, db, name); err != nil {
		return closeWithError(err)
	}
	if err := verifyForeignKeyIntegrity(ctx, db, name); err != nil {
		return closeWithError(err)
	}
	if err := secureDatabaseArtifacts(path); err != nil {
		return closeWithError(err)
	}
	return db, nil
}
func prepareDatabaseFile(path, name string) (fileIdentity, bool, error) {
	if info, err := os.Lstat(path); err == nil {
		identity, err := validateOwnedRegularFile(path, info, 0o600)
		if err != nil {
			return fileIdentity{}, false, fmt.Errorf("validate %s database: %w", name, err)
		}
		return identity, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fileIdentity{}, false, fmt.Errorf("inspect %s database: %w", name, err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fileIdentity{}, false, fmt.Errorf("create %s database: %w", name, err)
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return fileIdentity{}, false, fmt.Errorf("stat new %s database: %w", name, statErr)
	}
	if closeErr != nil {
		return fileIdentity{}, false, fmt.Errorf("close new %s database: %w", name, closeErr)
	}
	if _, err := validateOwnedRegularFile(path, info, 0o600); err != nil {
		return fileIdentity{}, false, fmt.Errorf("validate new %s database: %w", name, err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return fileIdentity{}, false, fmt.Errorf("sync new %s database directory entry: %w", name, err)
	}
	identity, err := identityFromInfo(info)
	return identity, true, err
}

func configureDatabase(ctx context.Context, db *sql.DB, name string) error {
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA synchronous = FULL`,
		`PRAGMA trusted_schema = OFF`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure %s database: %w", name, err)
		}
	}
	var journalMode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil {
		return fmt.Errorf("enable %s WAL mode: %w", name, err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("enable %s WAL mode: sqlite returned %q", name, journalMode)
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB, dataset string) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrationFiles)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, filepath.ToSlash(filepath.Join("migrations", dataset))); err != nil {
		return fmt.Errorf("migrate %s database: %w", dataset, err)
	}
	return nil
}

func verifyDatabaseIntegrity(ctx context.Context, db *sql.DB, dataset string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("run %s database integrity check: %w", dataset, err)
	}
	defer rows.Close()

	resultCount := 0
	healthy := true
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("read %s database integrity check: %w", dataset, err)
		}
		resultCount++
		if result != "ok" {
			healthy = false
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s database integrity check: %w", dataset, err)
	}
	if resultCount != 1 || !healthy {
		return fmt.Errorf("%s database integrity check failed", dataset)
	}
	return nil
}

func verifyForeignKeyIntegrity(ctx context.Context, db *sql.DB, dataset string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("run %s foreign key check: %w", dataset, err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("%s database foreign key check failed", dataset)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s foreign key check: %w", dataset, err)
	}
	return nil
}

func verifyCurrentSchema(ctx context.Context, db *sql.DB, dataset string) error {
	actual, err := readSchemaObjects(ctx, db)
	if err != nil {
		return fmt.Errorf("read %s database schema manifest: %w", dataset, err)
	}

	expectedDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return fmt.Errorf("create expected %s schema: %w", dataset, err)
	}
	defer expectedDB.Close()
	expectedDB.SetMaxOpenConns(1)
	if err := expectedDB.PingContext(ctx); err != nil {
		return fmt.Errorf("open expected %s schema: %w", dataset, err)
	}
	if err := migrate(ctx, expectedDB, dataset); err != nil {
		return fmt.Errorf("build expected %s schema: %w", dataset, err)
	}
	expected, err := readSchemaObjects(ctx, expectedDB)
	if err != nil {
		return fmt.Errorf("read expected %s schema manifest: %w", dataset, err)
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("%s database schema manifest mismatch", dataset)
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return fmt.Errorf("%s database schema manifest mismatch", dataset)
		}
	}
	return nil
}

func readSchemaObjects(ctx context.Context, db *sql.DB) ([]schemaObject, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT type, name, tbl_name, COALESCE(sql, '')
		FROM sqlite_schema
		WHERE name NOT LIKE 'sqlite_%'
		ORDER BY type, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	objects := []schemaObject{}
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.kind, &object.name, &object.table, &object.sql); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, rows.Err()
}

func preflightExistingDataset(ctx context.Context, path, expected string) error {
	query := make(url.Values)
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("inspect existing %s database: %w", expected, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("inspect existing %s database: %w", expected, err)
	}
	if err := verifyDatabaseIntegrity(ctx, db, expected); err != nil {
		return err
	}
	var metadataTables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'dataset_metadata'`).Scan(&metadataTables); err != nil {
		return fmt.Errorf("inspect existing %s database schema: %w", expected, err)
	}
	if metadataTables == 1 {
		dataset, schemaVersion, err := readDatasetIdentity(ctx, db)
		if err != nil {
			return fmt.Errorf("read existing %s dataset identity: %w", expected, err)
		}
		if dataset != expected {
			return fmt.Errorf("dataset identity mismatch: expected %q, found %q", expected, dataset)
		}
		if schemaVersion < 1 {
			return fmt.Errorf("existing %s dataset has invalid schema identity version %d", expected, schemaVersion)
		}
		return nil
	}

	var unidentifiedObjects int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM sqlite_schema
		WHERE name NOT LIKE 'sqlite_%'
		  AND name <> 'goose_db_version'
	`).Scan(&unidentifiedObjects); err != nil {
		return fmt.Errorf("inspect unidentified %s database: %w", expected, err)
	}
	if unidentifiedObjects != 0 {
		return fmt.Errorf("existing %s database is non-empty but has no dataset identity", expected)
	}
	return nil
}

func readDatasetIdentity(ctx context.Context, db *sql.DB) (string, int64, error) {
	var dataset string
	var schemaVersion int64
	if err := db.QueryRowContext(ctx, `SELECT dataset, schema_version FROM dataset_metadata WHERE singleton = 1`).Scan(&dataset, &schemaVersion); err != nil {
		return "", 0, err
	}
	return dataset, schemaVersion, nil
}

func verifyDatasetIdentity(ctx context.Context, db *sql.DB, expected string) error {
	dataset, schemaVersion, err := readDatasetIdentity(ctx, db)
	if err != nil {
		return fmt.Errorf("read %s dataset identity: %w", expected, err)
	}
	if dataset != expected {
		return fmt.Errorf("dataset identity mismatch: expected %q, found %q", expected, dataset)
	}
	migrationVersion, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("read %s migration version: %w", expected, err)
	}
	if schemaVersion != migrationVersion {
		return fmt.Errorf("%s schema version mismatch: metadata=%d migration=%d", expected, schemaVersion, migrationVersion)
	}
	return nil
}

func validateDatabaseArtifacts(path string) error {
	for _, artifact := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		info, err := os.Lstat(artifact)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect database artifact %s: %w", artifact, err)
		}
		if _, err := validateOwnedRegularFile(artifact, info, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func secureDatabaseArtifacts(path string) error {
	for _, artifact := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		info, err := os.Lstat(artifact)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect database artifact %s: %w", artifact, err)
		}
		if _, err := validateOwnedRegularFile(artifact, info, 0); err != nil {
			return err
		}
		if err := os.Chmod(artifact, 0o600); err != nil {
			return fmt.Errorf("secure database artifact %s: %w", artifact, err)
		}
	}
	return nil
}

func validateTrustedDirectoryChain(path string) ([]trustedDirectory, error) {
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return nil, errors.New("trusted directory chain must be absolute")
	}

	paths := []string{}
	for current := cleaned; ; current = filepath.Dir(current) {
		paths = append(paths, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for left, right := 0, len(paths)-1; left < right; left, right = left+1, right-1 {
		paths[left], paths[right] = paths[right], paths[left]
	}

	chain := make([]trustedDirectory, 0, len(paths))
	for _, directoryPath := range paths {
		info, err := os.Lstat(directoryPath)
		if err != nil {
			return nil, fmt.Errorf("inspect ancestor %s: %w", directoryPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("ancestor is not a real directory: %s", directoryPath)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil, fmt.Errorf("cannot inspect ancestor ownership: %s", directoryPath)
		}
		if int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid() {
			return nil, fmt.Errorf("ancestor is not owned by root or the current uid: %s", directoryPath)
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return nil, fmt.Errorf("ancestor is writable by group or other users without sticky protection: %s", directoryPath)
		}
		chain = append(chain, trustedDirectory{
			path:     directoryPath,
			identity: fileIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)},
			mode:     info.Mode().Perm(),
			uid:      stat.Uid,
		})
	}
	return chain, nil
}

func sameTrustedDirectoryChain(left, right []trustedDirectory) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func secureOwnedPath(path string, directory bool) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("path must not be a symlink: %s", path)
	}
	if directory {
		if !info.IsDir() {
			return nil, fmt.Errorf("path is not a directory: %s", path)
		}
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("cannot inspect owner for path: %s", path)
	}
	if int(stat.Uid) != os.Geteuid() {
		return nil, fmt.Errorf("path is not owned by the current uid: %s", path)
	}
	return info, nil
}

func validateOwnedRegularFile(path string, info os.FileInfo, requiredMode os.FileMode) (fileIdentity, error) {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fileIdentity{}, fmt.Errorf("database artifact is not a regular file: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, fmt.Errorf("cannot inspect database artifact identity: %s", path)
	}
	if int(stat.Uid) != os.Geteuid() {
		return fileIdentity{}, fmt.Errorf("database artifact is not owned by the current uid: %s", path)
	}
	if uint64(stat.Nlink) != 1 {
		return fileIdentity{}, fmt.Errorf("database artifact must have exactly one hard link: %s", path)
	}
	if requiredMode != 0 && info.Mode().Perm() != requiredMode {
		return fileIdentity{}, fmt.Errorf("database artifact permissions must be %04o, found %04o: %s", requiredMode, info.Mode().Perm(), path)
	}
	return fileIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func identityFromInfo(info os.FileInfo) (fileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, errors.New("cannot inspect database file identity")
	}
	return fileIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func requireSamePathIdentity(path string, expected fileIdentity) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	actual, err := validateOwnedRegularFile(path, info, 0o600)
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("database path device/inode changed")
	}
	return nil
}
