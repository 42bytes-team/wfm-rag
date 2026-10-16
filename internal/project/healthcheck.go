package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"wfm-rag/internal/index"
)

// Finding is one healthcheck result. Findings are data on stdout, never errors.
type Finding struct {
	Kind    string `json:"kind"` // root_missing | index_missing | index_invalid | index_dangling | state_unexpected
	Project string `json:"project,omitempty"`
	Path    string `json:"path,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// Healthcheck sweeps the registry and the state directory without writing anything.
// Sweep 1: registered roots are live directories. Sweep 2: derived index files
// exist as regular files; verify=true additionally runs the full root-bound
// index.Load per project. Sweep 3: index directories under the state directory
// must be claimed by a registered project. Findings are sorted by kind, project, path.
func Healthcheck(registry Registry, verify bool) ([]Finding, error) {
	findings := []Finding{}
	claimed := make(map[string]bool, len(registry.Projects))
	for _, id := range sortedIDs(registry) {
		entry := registry.Projects[id]
		indexPath, err := index.DefaultPathFromCanonical(entry.Root)
		if err != nil {
			findings = append(findings, Finding{
				Kind: "index_invalid", Project: id, Path: entry.Root, Detail: err.Error(),
			})
			continue
		}
		claimed[filepath.Dir(indexPath)] = true
		if info, statErr := os.Stat(entry.Root); statErr != nil || !info.IsDir() {
			findings = append(findings, Finding{
				Kind: "root_missing", Project: id, Path: entry.Root,
				Detail: "registered root is not an existing directory",
			})
		}
		info, statErr := os.Lstat(indexPath)
		switch {
		case statErr != nil || !info.Mode().IsRegular():
			findings = append(findings, Finding{
				Kind: "index_missing", Project: id, Path: indexPath,
				Detail: "no regular index file at the derived path",
			})
		case verify:
			if _, loadErr := index.Load(indexPath, entry.Root); loadErr != nil {
				findings = append(findings, Finding{
					Kind: "index_invalid", Project: id, Path: indexPath, Detail: loadErr.Error(),
				})
			}
		}
	}
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read state directory %q: %w", dir, err)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case entry.Name() == registryName || entry.Name() == lockName:
			continue
		case entry.IsDir():
			if !claimed[path] {
				findings = append(findings, Finding{
					Kind: "index_dangling", Path: path,
					Detail: "index directory is not claimed by any registered project",
				})
			}
		case entry.Type().IsRegular():
			findings = append(findings, Finding{
				Kind: "state_unexpected", Path: path,
				Detail: "unexpected regular file in state directory",
			})
		default:
			findings = append(findings, Finding{
				Kind: "state_unexpected", Path: path,
				Detail: "unexpected entry type in state directory",
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Project != right.Project {
			return left.Project < right.Project
		}
		return left.Path < right.Path
	})
	return findings, nil
}

// PurgeDangling deletes dangling index directories that contain exactly one
// regular index.json. Anything stranger is reported in findings and preserved.
// It never touches the registry, the lock, claimed directories, or target repositories.
func PurgeDangling(registry Registry) ([]string, []Finding, error) {
	var purged []string
	var retained []Finding
	err := withLock(func() error {
		findings, err := Healthcheck(registry, false)
		if err != nil {
			return err
		}
		for _, finding := range findings {
			if finding.Kind != "index_dangling" {
				continue
			}
			if reason, ok := purgeable(finding.Path); !ok {
				finding.Detail = "not purged: " + reason
				retained = append(retained, finding)
				continue
			}
			filePath := filepath.Join(finding.Path, "index.json")
			if err := os.Remove(filePath); err != nil {
				return fmt.Errorf("remove dangling index %q: %w", filePath, err)
			}
			if err := os.Remove(finding.Path); err != nil {
				return fmt.Errorf("remove dangling index directory %q: %w", finding.Path, err)
			}
			purged = append(purged, finding.Path)
		}
		return nil
	})
	sort.Strings(purged)
	return purged, retained, err
}

func purgeable(dir string) (string, bool) {
	info, err := os.Lstat(dir)
	if err != nil {
		return err.Error(), false
	}
	if !info.IsDir() {
		return "entry is not a directory", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err.Error(), false
	}
	if len(entries) != 1 || entries[0].Name() != "index.json" || !entries[0].Type().IsRegular() {
		return "expected exactly one regular index.json", false
	}
	return "", true
}
