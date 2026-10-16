// Package scaffold owns the knowledge-base skeleton that `project init`
// bootstraps inside a target repository: category directories, schema.md,
// card templates, and a starter agent-knowledge skill. It creates missing
// files only and never overwrites, deletes, or rewrites existing content.
package scaffold

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed assets/schema.md assets/skill.md assets/templates/*.md
var assets embed.FS

const (
	projectPlaceholder = "{{PROJECT}}"

	knowledgeDir = ".agents/knowledge"
	skillFile    = ".agents/skills/agent-knowledge/SKILL.md"
)

// Categories mirrors the indexed corpus layout in internal/cards.
var categories = []string{"anti-patterns", "dependencies", "incidents", "patterns", "research", "sources", "state"}

// templateFiles are the embedded card templates copied to .agents/knowledge/templates/.
var templateFiles = []string{
	"anti-pattern.md", "decision.md", "dependency.md", "incident.md",
	"pattern.md", "reference.md", "research.md", "state.md",
}

// Ensure creates every missing skeleton path under root and returns the
// created repository-relative paths, sorted. Existing files, directories,
// and symlinks are left untouched. A skeleton path that exists as the wrong
// kind of entry (file where a directory is required, or a symlinked
// .agents/.agents/knowledge) is an error: the skeleton is never written
// through links and never displaces operator content.
func Ensure(root, projectID string) ([]string, error) {
	if projectID == "" {
		return nil, errors.New("scaffold: project ID is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("scaffold: resolve repository root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scaffold: repository root %q is not a directory", root)
	}
	for _, dir := range []string{".agents", knowledgeDir} {
		if err := requireRealDir(root, dir); err != nil {
			return nil, err
		}
	}

	var created []string
	dirs := make([]string, 0, len(categories)+3)
	dirs = append(dirs, knowledgeDir, filepath.Join(knowledgeDir, "templates"), filepath.Dir(skillFile))
	for _, category := range categories {
		dirs = append(dirs, filepath.Join(knowledgeDir, category))
	}
	for _, dir := range dirs {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, fmt.Errorf("scaffold: create %q: %w", dir, err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("scaffold: %q exists but is not a directory", dir)
		}
	}

	files := map[string]string{
		filepath.Join(knowledgeDir, "schema.md"): "assets/schema.md",
		skillFile:                                "assets/skill.md",
	}
	for _, name := range templateFiles {
		files[filepath.Join(knowledgeDir, "templates", name)] = "assets/templates/" + name
	}
	for _, rel := range sortedKeys(files) {
		asset := files[rel]
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("scaffold: inspect %q: %w", rel, err)
		}
		content, err := assets.ReadFile(asset)
		if err != nil {
			return nil, fmt.Errorf("scaffold: read embedded asset %q: %w", asset, err)
		}
		rendered := strings.ReplaceAll(string(content), projectPlaceholder, projectID)
		path := filepath.Join(root, rel)
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			return nil, fmt.Errorf("scaffold: create %q: %w", rel, err)
		}
		created = append(created, rel)
	}
	sort.Strings(created)
	return created, nil
}

func requireRealDir(root, rel string) error {
	info, err := os.Lstat(filepath.Join(root, rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scaffold: inspect %q: %w", rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("scaffold: %q is a symlink; the skeleton is never written through links", rel)
	}
	if !info.IsDir() {
		return fmt.Errorf("scaffold: %q exists but is not a directory", rel)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ = fs.FS(assets)
