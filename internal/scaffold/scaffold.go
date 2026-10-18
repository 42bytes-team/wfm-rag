// Package scaffold owns the knowledge-base skeleton that `project init`
// bootstraps inside a target repository: category directories, a knowledge-base
// README carrying the profile-filling prompt, schema.md, card templates, and a
// starter agent-knowledge skill. It creates missing files only and never
// overwrites, deletes, or rewrites existing content.
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

//go:embed assets/kb-readme.md assets/schema.md assets/skill.md assets/templates/*.md
var assets embed.FS

const (
	projectPlaceholder = "{{PROJECT}}"

	knowledgeDir = ".agents/knowledge"

	// KBReadmeFile and SkillFile are exported so `project init` can name the
	// profile prompt and the file it fills in its next-step output.
	KBReadmeFile = knowledgeDir + "/README.md"
	SkillFile    = ".agents/skills/agent-knowledge/SKILL.md"

	profileTermsPlaceholder    = "<project-specific terms>"
	profileCommandsPlaceholder = "<project-specific commands>"
	promptMarker               = "First Task For The Agent"
)

// Categories mirrors the indexed corpus layout in internal/cards.
var categories = []string{"anti-patterns", "dependencies", "incidents", "patterns", "research", "sources", "state"}

// templateFiles are the embedded card templates copied to .agents/knowledge/templates/.
var templateFiles = []string{
	"anti-pattern.md", "decision.md", "dependency.md", "incident.md",
	"pattern.md", "reference.md", "research.md", "state.md",
}

// Ensure creates every missing skeleton path under root and returns the
// created repository-relative paths, sorted. Every operation is anchored to an
// opened root descriptor, so no write can leave the repository. Each skeleton
// directory component is created and re-checked individually, so an existing
// symlink or non-directory entry is an error instead of a path that gets
// followed. File creation is exclusive (`O_CREATE|O_EXCL`): an existing regular
// file is left untouched, while a directory or symlink at a file destination is
// an error. A file that fails mid-write is removed, so a later run never
// accepts a truncated scaffold asset as operator content.
func Ensure(root, projectID string) ([]string, error) {
	if projectID == "" {
		return nil, errors.New("scaffold: project ID is required")
	}
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("scaffold: open repository root %q: %w", root, err)
	}
	defer rootDir.Close()

	dirs := make([]string, 0, len(categories)+4)
	dirs = append(dirs, ".agents", knowledgeDir, filepath.Join(knowledgeDir, "templates"), filepath.Dir(SkillFile))
	for _, category := range categories {
		dirs = append(dirs, filepath.Join(knowledgeDir, category))
	}
	for _, dir := range dirs {
		if err := ensureRealDir(rootDir, dir); err != nil {
			return nil, err
		}
	}

	files := map[string]string{
		KBReadmeFile:                             "assets/kb-readme.md",
		filepath.Join(knowledgeDir, "schema.md"): "assets/schema.md",
		SkillFile:                                "assets/skill.md",
	}
	for _, name := range templateFiles {
		files[filepath.Join(knowledgeDir, "templates", name)] = "assets/templates/" + name
	}
	var created []string
	for _, rel := range sortedKeys(files) {
		content, err := assets.ReadFile(files[rel])
		if err != nil {
			return nil, fmt.Errorf("scaffold: read embedded asset %q: %w", files[rel], err)
		}
		rendered := strings.ReplaceAll(string(content), projectPlaceholder, projectID)
		wrote, err := createExclusive(rootDir, rel, rendered)
		if errors.Is(err, errExists) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("scaffold: %w", err)
		}
		if wrote {
			created = append(created, rel)
		}
	}
	sort.Strings(created)
	return created, nil
}

// ProfileStatus describes the starter profile state that `project init`
// reports to the operator.
type ProfileStatus struct {
	// Incomplete is true while the starter skill still carries the unfilled
	// profile placeholders.
	Incomplete bool
	// PromptFile names the knowledge README when that file carries the
	// scaffolded profile prompt, and is empty when no scaffolded prompt exists.
	PromptFile string
}

// ReadProfileStatus inspects the starter skill and the knowledge README under
// root. A missing starter skill counts as incomplete because `Ensure` creates
// it, so an absent file means the profile was never filled. A customized
// README that does not carry the scaffolded prompt is never named.
func ReadProfileStatus(root string) (ProfileStatus, error) {
	var status ProfileStatus
	skill, err := os.ReadFile(filepath.Join(root, SkillFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		status.Incomplete = true
	case err != nil:
		return status, fmt.Errorf("inspect %q: %w", SkillFile, err)
	default:
		text := string(skill)
		status.Incomplete = strings.Contains(text, profileTermsPlaceholder) ||
			strings.Contains(text, profileCommandsPlaceholder)
	}
	readme, err := os.ReadFile(filepath.Join(root, KBReadmeFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return status, fmt.Errorf("inspect %q: %w", KBReadmeFile, err)
	}
	if hasScaffoldedPrompt(string(readme)) {
		status.PromptFile = KBReadmeFile
	}
	return status, nil
}

// hasScaffoldedPrompt requires the prompt heading and both profile
// placeholders, so a customized README that keeps the heading but drops the
// prompt body is not named as the prompt source.
func hasScaffoldedPrompt(text string) bool {
	return strings.Contains(text, promptMarker) &&
		strings.Contains(text, profileTermsPlaceholder) &&
		strings.Contains(text, profileCommandsPlaceholder)
}

var errExists = errors.New("exists")

// createExclusive writes content to rel inside rootDir only when that path does
// not exist. It reports whether it wrote. An existing regular file is left
// untouched; an existing symlink, directory, or other entry is an error. A
// failed write or close removes the file this call created.
func createExclusive(rootDir *os.Root, rel, content string) (bool, error) {
	file, err := rootDir.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		info, statErr := rootDir.Lstat(rel)
		if statErr != nil {
			return false, fmt.Errorf("create %q: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("%q is a symlink; the skeleton is never written through links", rel)
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("%q exists but is not a regular file", rel)
		}
		return false, errExists
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = rootDir.Remove(rel)
		return false, fmt.Errorf("write %q: %w", rel, err)
	}
	if err := file.Close(); err != nil {
		_ = rootDir.Remove(rel)
		return false, fmt.Errorf("close %q: %w", rel, err)
	}
	return true, nil
}

// ensureRealDir creates each missing component of rel and confirms that every
// component is a real directory, so an existing symlink is rejected instead of
// followed and no directory is created through one.
func ensureRealDir(rootDir *os.Root, rel string) error {
	prefix := ""
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if prefix != "" {
			prefix += "/"
		}
		prefix += part
		info, err := rootDir.Lstat(prefix)
		if errors.Is(err, os.ErrNotExist) {
			if err := rootDir.Mkdir(prefix, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create %q: %w", prefix, err)
			}
			info, err = rootDir.Lstat(prefix)
		}
		if err != nil {
			return fmt.Errorf("inspect %q: %w", prefix, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%q is a symlink; the skeleton is never written through links", prefix)
		}
		if !info.IsDir() {
			return fmt.Errorf("%q exists but is not a directory", prefix)
		}
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
