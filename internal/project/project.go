// Package project owns the operator-local registry that maps project IDs to
// canonical repository roots, its locked atomic persistence, and state-directory maintenance.
package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"

	"wfm-rag/internal/cards"
	"wfm-rag/internal/index"
)

const (
	// MaxRegistryBytes bounds the registry file size.
	MaxRegistryBytes = 1 << 20
	// MaxProjects bounds registry entries and, by contract, one MCP process.
	MaxProjects = 64

	registryVersion = 1
	registryName    = "projects.json"
	lockName        = "projects.lock"
)

var (
	ErrRegistryMissing = errors.New("project registry missing")
	ErrRegistryInvalid = errors.New("project registry invalid")
	ErrIDInvalid       = errors.New("project ID invalid")
	ErrUnknownID       = errors.New("project not registered")
	ErrLimitExceeded   = errors.New("project limit exceeded")
	ErrLocked          = errors.New("project registry locked")
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Entry is one registered project. Only the canonical root is stored; index
// paths are derived by index.DefaultPathFromCanonical and limits have authoritative owners elsewhere.
type Entry struct {
	Root string
}

// Registry is the parsed identity map. Projects may be held in any order;
// persistence always renders them sorted by ID.
type Registry struct {
	Version  int
	Projects map[string]Entry
}

// InitResult reports one registry mutation.
type InitResult struct {
	ID           string
	Root         string
	PreviousRoot string // non-empty only when Rebound is true
	Rebound      bool   // the ID moved from a different directory
	Created      bool   // the ID was not previously registered
}

// Status is one registry entry with cheap liveness facts. Status computation
// never opens an index file.
type Status struct {
	ID           string `json:"project"`
	Root         string `json:"root"`
	IndexPath    string `json:"index_path"`
	RootMissing  bool   `json:"root_missing"`
	IndexMissing bool   `json:"index_missing"`
}

type registryFile struct {
	Version  int                  `json:"version"`
	Projects map[string]entryFile `json:"projects"`
}

type entryFile struct {
	Root string `json:"root"`
}

// RegistryPath returns the canonical registry location inside the state directory.
func RegistryPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", fmt.Errorf("resolve project registry path: %w", err)
	}
	return filepath.Join(dir, registryName), nil
}

// stateDir returns the shared state directory: $XDG_DATA_HOME/wfm-rag when
// XDG_DATA_HOME is set and absolute, otherwise $HOME/.local/share/wfm-rag.
// A relative or empty XDG_DATA_HOME is invalid per the XDG Base Directory
// spec and falls back. An empty HOME is an error.
func stateDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "wfm-rag"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve state directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "wfm-rag"), nil
}

// Load reads and strictly validates the registry.
func Load() (Registry, error) {
	path, err := RegistryPath()
	if err != nil {
		return Registry{}, err
	}
	return loadFile(path)
}

func loadFile(path string) (Registry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Registry{}, fmt.Errorf("%w: %s", ErrRegistryMissing, path)
		}
		return Registry{}, fmt.Errorf("%w: %s: %v", ErrRegistryInvalid, path, err)
	}
	if !info.Mode().IsRegular() {
		return Registry{}, fmt.Errorf("%w: %s is not a regular file", ErrRegistryInvalid, path)
	}
	if info.Size() > MaxRegistryBytes {
		return Registry{}, fmt.Errorf("%w: %s exceeds %d bytes", ErrRegistryInvalid, path, MaxRegistryBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Registry{}, fmt.Errorf("read project registry %q: %w", path, err)
	}
	return parseRegistry(data, path)
}

func parseRegistry(data []byte, path string) (Registry, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	projects, err := decodeProjectsContainer(decoder)
	if err != nil {
		return Registry{}, fmt.Errorf("%w: %s: %v", ErrRegistryInvalid, path, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Registry{}, fmt.Errorf("%w: %s: trailing data after the registry object", ErrRegistryInvalid, path)
	}
	registry := Registry{Version: registryVersion, Projects: projects}
	if err := validateRegistry(registry, path); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

func decodeProjectsContainer(decoder *json.Decoder) (map[string]Entry, error) {
	if err := openObject(decoder); err != nil {
		return nil, err
	}
	versionSeen := false
	projectsSeen := false
	projects := map[string]Entry{}
	for decoder.More() {
		key, err := decodeKey(decoder)
		if err != nil {
			return nil, err
		}
		switch key {
		case "version":
			if versionSeen {
				return nil, fmt.Errorf("duplicate key %q", key)
			}
			versionSeen = true
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			number, isNumber := token.(float64)
			if !isNumber || number != registryVersion {
				return nil, fmt.Errorf("version must be %d", registryVersion)
			}
		case "projects":
			if projectsSeen {
				return nil, fmt.Errorf("duplicate key %q", key)
			}
			projectsSeen = true
			entries, err := decodeProjects(decoder)
			if err != nil {
				return nil, err
			}
			projects = entries
		default:
			if err := skipValue(decoder); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("unknown field %q", key)
		}
	}
	if err := closeObject(decoder); err != nil {
		return nil, err
	}
	if !versionSeen {
		return nil, fmt.Errorf("field %q is required", "version")
	}
	if !projectsSeen {
		return nil, fmt.Errorf("field %q is required", "projects")
	}
	return projects, nil
}

func decodeProjects(decoder *json.Decoder) (map[string]Entry, error) {
	if err := openObject(decoder); err != nil {
		return nil, err
	}
	entries := map[string]Entry{}
	for decoder.More() {
		id, err := decodeKey(decoder)
		if err != nil {
			return nil, err
		}
		if _, seen := entries[id]; seen {
			return nil, fmt.Errorf("duplicate project key %q", id)
		}
		entry, err := decodeEntry(decoder)
		if err != nil {
			return nil, fmt.Errorf("project %q: %v", id, err)
		}
		entries[id] = entry
	}
	if err := closeObject(decoder); err != nil {
		return nil, err
	}
	return entries, nil
}

func decodeEntry(decoder *json.Decoder) (Entry, error) {
	if err := openObject(decoder); err != nil {
		return Entry{}, err
	}
	root := ""
	rootSeen := false
	for decoder.More() {
		key, err := decodeKey(decoder)
		if err != nil {
			return Entry{}, err
		}
		switch key {
		case "root":
			if rootSeen {
				return Entry{}, fmt.Errorf("duplicate key %q", key)
			}
			rootSeen = true
			value, err := decodeString(decoder)
			if err != nil {
				return Entry{}, err
			}
			root = value
		default:
			if err := skipValue(decoder); err != nil {
				return Entry{}, err
			}
			return Entry{}, fmt.Errorf("unknown field %q", key)
		}
	}
	if err := closeObject(decoder); err != nil {
		return Entry{}, err
	}
	if !rootSeen {
		return Entry{}, fmt.Errorf("field %q is required", "root")
	}
	return Entry{Root: root}, nil
}

func openObject(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, isDelim := token.(json.Delim); !isDelim || delim != '{' {
		return fmt.Errorf("expected a JSON object")
	}
	return nil
}

func closeObject(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, isDelim := token.(json.Delim); !isDelim || delim != '}' {
		return fmt.Errorf("expected the object to close")
	}
	return nil
}

func decodeKey(decoder *json.Decoder) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	key, isString := token.(string)
	if !isString {
		return "", fmt.Errorf("expected an object key")
	}
	return key, nil
}

func decodeString(decoder *json.Decoder) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	value, isString := token.(string)
	if !isString {
		return "", fmt.Errorf("expected a string value")
	}
	return value, nil
}

func skipValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if _, isDelim := token.(json.Delim); !isDelim {
		return nil
	}
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if nested, isDelim := token.(json.Delim); isDelim {
			switch nested {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

func validateRegistry(registry Registry, path string) error {
	if len(registry.Projects) > MaxProjects {
		return fmt.Errorf("%w: %s holds %d projects, the limit is %d",
			ErrLimitExceeded, path, len(registry.Projects), MaxProjects)
	}
	for id, entry := range registry.Projects {
		if !idPattern.MatchString(id) {
			return fmt.Errorf("%w: %s stores project ID %q, which must match %s",
				ErrRegistryInvalid, path, id, idPattern.String())
		}
		if entry.Root == "" || !filepath.IsAbs(entry.Root) {
			return fmt.Errorf("%w: %s stores project %q with a non-absolute root %q",
				ErrRegistryInvalid, path, id, entry.Root)
		}
	}
	return nil
}

// Init registers id at dir, or rebinds an existing ID to dir and reports the
// previous root. Re-registering the same directory is a no-op refresh.
func Init(id, dir string) (InitResult, error) {
	if !idPattern.MatchString(id) {
		return InitResult{}, fmt.Errorf("%w: %q must match %s", ErrIDInvalid, id, idPattern.String())
	}
	canonical, err := cards.CanonicalRoot(dir)
	if err != nil {
		return InitResult{}, fmt.Errorf("resolve project root: %w", err)
	}
	var result InitResult
	err = withLock(func() error {
		path, err := RegistryPath()
		if err != nil {
			return err
		}
		registry, err := loadOrEmpty(path)
		if err != nil {
			return err
		}
		previous, existed := registry.Projects[id]
		if !existed && len(registry.Projects) >= MaxProjects {
			return fmt.Errorf("%w: registry already holds %d projects", ErrLimitExceeded, MaxProjects)
		}
		registry.Projects[id] = Entry{Root: canonical}
		result = InitResult{ID: id, Root: canonical, Created: !existed}
		if existed && previous.Root != canonical {
			result.Rebound = true
			result.PreviousRoot = previous.Root
		}
		return saveLocked(registry)
	})
	if err != nil {
		return InitResult{}, err
	}
	return result, nil
}

// Remove deletes one registry entry. It never touches repository or index files.
func Remove(id string) (InitResult, error) {
	if !idPattern.MatchString(id) {
		return InitResult{}, fmt.Errorf("%w: %q must match %s", ErrIDInvalid, id, idPattern.String())
	}
	var result InitResult
	err := withLock(func() error {
		path, err := RegistryPath()
		if err != nil {
			return err
		}
		registry, err := loadFile(path)
		if err != nil {
			return err
		}
		entry, ok := registry.Projects[id]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownID, id)
		}
		delete(registry.Projects, id)
		result = InitResult{ID: id, Root: entry.Root}
		return saveLocked(registry)
	})
	if err != nil {
		return InitResult{}, err
	}
	return result, nil
}

// Statuses reports every entry with cheap liveness facts, sorted by ID.
func Statuses(registry Registry) ([]Status, error) {
	statuses := make([]Status, 0, len(registry.Projects))
	for _, id := range sortedIDs(registry) {
		entry := registry.Projects[id]
		indexPath, err := index.DefaultPathFromCanonical(entry.Root)
		if err != nil {
			return nil, fmt.Errorf("derive index path for project %q: %w", id, err)
		}
		status := Status{ID: id, Root: entry.Root, IndexPath: indexPath}
		if info, err := os.Stat(entry.Root); err != nil || !info.IsDir() {
			status.RootMissing = true
		}
		if info, err := os.Lstat(indexPath); err != nil || !info.Mode().IsRegular() {
			status.IndexMissing = true
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// ProjectPaths returns the derived index file path for every registered
// project, sorted. Adapters use it to claim which index files belong to the
// registry.
func ProjectPaths(registry Registry) ([]string, error) {
	seen := make(map[string]bool, len(registry.Projects))
	paths := make([]string, 0, len(registry.Projects))
	for _, entry := range registry.Projects {
		path, err := index.DefaultPathFromCanonical(entry.Root)
		if err != nil {
			return nil, fmt.Errorf("derive index path for project root %q: %w", entry.Root, err)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func loadOrEmpty(path string) (Registry, error) {
	registry, err := loadFile(path)
	if errors.Is(err, ErrRegistryMissing) {
		return Registry{Version: registryVersion, Projects: map[string]Entry{}}, nil
	}
	return registry, err
}

func withLock(run func() error) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := ensureStateDir(dir); err != nil {
		return err
	}
	lockPath := filepath.Join(dir, lockName)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open registry lock %q: %w", lockPath, err)
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("%w: %s is held by another writer", ErrLocked, lockPath)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	return run()
}

func ensureStateDir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory %q: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("set state directory permissions %q: %w", dir, err)
	}
	return nil
}

func saveLocked(registry Registry) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	file := registryFile{Version: registryVersion, Projects: make(map[string]entryFile, len(registry.Projects))}
	for id, entry := range registry.Projects {
		file.Projects[id] = entryFile{Root: entry.Root}
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project registry: %w", err)
	}
	data = append(data, '\n')
	if len(data) > MaxRegistryBytes {
		return fmt.Errorf("%w: rendered registry exceeds %d bytes", ErrLimitExceeded, MaxRegistryBytes)
	}
	path := filepath.Join(dir, registryName)
	temporary, err := os.CreateTemp(dir, "."+registryName+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary registry beside %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			temporary.Close()
			os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary registry permissions %q: %w", temporaryPath, err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary registry %q: %w", temporaryPath, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary registry %q: %w", temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary registry %q: %w", temporaryPath, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace registry %q: %w", path, err)
	}
	keepTemporary = false
	directoryFile, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open state directory %q for sync: %w", dir, err)
	}
	defer directoryFile.Close()
	if err := directoryFile.Sync(); err != nil {
		return fmt.Errorf("sync state directory %q: %w", dir, err)
	}
	return nil
}

func sortedIDs(registry Registry) []string {
	ids := make([]string, 0, len(registry.Projects))
	for id := range registry.Projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
