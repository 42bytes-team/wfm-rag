package scaffold

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func expectedFiles() []string {
	files := []string{
		KBReadmeFile,
		filepath.Join(knowledgeDir, "schema.md"),
		SkillFile,
	}
	for _, name := range templateFiles {
		files = append(files, filepath.Join(knowledgeDir, "templates", name))
	}
	sort.Strings(files)
	return files
}

func TestEnsureCreatesFullSkeletonWithProjectID(t *testing.T) {
	root := t.TempDir()
	created, err := Ensure(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	want := expectedFiles()
	if len(created) != len(want) {
		t.Fatalf("created %d files, want %d: %v", len(created), len(want), created)
	}
	for i, rel := range want {
		if created[i] != rel {
			t.Errorf("created[%d] = %q, want %q", i, created[i], rel)
		}
	}
	for _, category := range categories {
		info, err := os.Stat(filepath.Join(root, knowledgeDir, category))
		if err != nil || !info.IsDir() {
			t.Errorf("category directory %q missing", category)
		}
	}
	schema, err := os.ReadFile(filepath.Join(root, knowledgeDir, "schema.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(schema), "{{PROJECT}}") {
		t.Error("schema.md still contains the raw placeholder")
	}
	if !strings.Contains(string(schema), "`api`") {
		t.Error("schema.md does not name the project ID")
	}
	skill, err := os.ReadFile(filepath.Join(root, SkillFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(skill), "{{PROJECT}}") || !strings.Contains(string(skill), "--project api") {
		t.Error("starter skill must render the project ID into every command")
	}
	readme, err := os.ReadFile(filepath.Join(root, KBReadmeFile))
	if err != nil {
		t.Fatalf("knowledge README missing: %v", err)
	}
	if strings.Contains(string(readme), "{{PROJECT}}") || !strings.Contains(string(readme), "`api`") {
		t.Error("knowledge README must name the project ID")
	}
	prompt := string(readme)
	for _, want := range []string{"First Task For The Agent", "Repository Profile", "<project-specific terms>", "<project-specific commands>"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("knowledge README prompt is missing %q", want)
		}
	}
}

func TestEnsureIsIdempotentAndNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	if _, err := Ensure(root, "api"); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(root, knowledgeDir, "schema.md")
	if err := os.WriteFile(schemaPath, []byte("operator content"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := Ensure(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 {
		t.Errorf("second Ensure created %v, want nothing", created)
	}
	kept, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "operator content" {
		t.Errorf("Ensure overwrote schema.md with %q", kept)
	}
}

func TestEnsureFillsOnlyMissingPieces(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, knowledgeDir, "patterns"), 0o755); err != nil {
		t.Fatal(err)
	}
	card := filepath.Join(root, knowledgeDir, "patterns", "existing.md")
	if err := os.WriteFile(card, []byte("# Existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := Ensure(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range created {
		if rel == filepath.Join(knowledgeDir, "patterns", "existing.md") {
			t.Fatalf("Ensure reported an existing card as created: %v", created)
		}
	}
	if len(created) != len(expectedFiles()) {
		t.Fatalf("created %d files, want all %d skeleton files", len(created), len(expectedFiles()))
	}
	kept, err := os.ReadFile(card)
	if err != nil || string(kept) != "# Existing\n" {
		t.Error("existing card was modified")
	}
}

func TestEnsureRejectsUnsafeSkeletonPaths(t *testing.T) {
	t.Run("symlinked .agents", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
			t.Fatal(err)
		}
		if _, err := Ensure(root, "api"); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Errorf("Ensure error = %v, want a symlink rejection", err)
		}
		if _, err := os.Lstat(filepath.Join(outside, "knowledge")); err == nil {
			t.Error("Ensure wrote through the symlinked .agents")
		}
	})
	t.Run("regular file where directory is required", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, knowledgeDir), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Ensure(root, "api"); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("Ensure error = %v, want a not-a-directory rejection", err)
		}
	})
}

func TestEnsureRequiresProjectID(t *testing.T) {
	if _, err := Ensure(t.TempDir(), ""); err == nil {
		t.Error("Ensure(\"\") error = nil, want an error for an empty project ID")
	}
}

func TestEnsureRejectsSymlinkedSkeletonDirectories(t *testing.T) {
	for _, link := range []string{".agents/knowledge/templates", ".agents/skills", ".agents/skills/agent-knowledge"} {
		t.Run(link, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, filepath.Dir(link)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, link)); err != nil {
				t.Fatal(err)
			}
			if _, err := Ensure(root, "api"); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Errorf("Ensure error = %v, want a symlink rejection for %s", err, link)
			}
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("Ensure wrote through the symlinked %s: %v", link, entries)
			}
		})
	}
}

func TestEnsureRejectsWrongKindFileDestinations(t *testing.T) {
	t.Run("directory at the knowledge README path", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, KBReadmeFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Ensure(root, "api"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("Ensure error = %v, want a wrong-kind rejection for a directory", err)
		}
	})
	t.Run("symlinked knowledge README", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "operator.md")
		if err := os.WriteFile(target, []byte("# Operator\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, knowledgeDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, KBReadmeFile)); err != nil {
			t.Fatal(err)
		}
		if _, err := Ensure(root, "api"); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Errorf("Ensure error = %v, want a symlink rejection", err)
		}
		kept, err := os.ReadFile(target)
		if err != nil || string(kept) != "# Operator\n" {
			t.Errorf("Ensure wrote through the symlinked README: %q %v", kept, err)
		}
	})
	t.Run("customized regular README is kept", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, knowledgeDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, KBReadmeFile), []byte("# Mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		created, err := Ensure(root, "api")
		if err != nil {
			t.Fatal(err)
		}
		for _, rel := range created {
			if rel == KBReadmeFile {
				t.Errorf("Ensure reported the customized README as created: %v", created)
			}
		}
		kept, err := os.ReadFile(filepath.Join(root, KBReadmeFile))
		if err != nil || string(kept) != "# Mine\n" {
			t.Errorf("Ensure overwrote the customized README: %q %v", kept, err)
		}
	})
}

func TestReadProfileStatusFollowsScaffoldedContent(t *testing.T) {
	root := t.TempDir()
	if _, err := Ensure(root, "api"); err != nil {
		t.Fatal(err)
	}
	status, err := ReadProfileStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Incomplete || status.PromptFile != KBReadmeFile {
		t.Errorf("status = %+v, want an incomplete profile naming the scaffolded prompt", status)
	}

	skill := filepath.Join(root, SkillFile)
	content, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	filled := strings.NewReplacer(
		"<project-specific terms>", "index dispatch",
		"<project-specific commands>", "go test ./...",
	).Replace(string(content))
	if err := os.WriteFile(skill, []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, err = ReadProfileStatus(root); err != nil {
		t.Fatal(err)
	}
	if status.Incomplete {
		t.Errorf("status = %+v, want the filled profile reported as complete", status)
	}

	customized := "## First Task For The Agent\n\nFill the profile yourself.\n"
	if err := os.WriteFile(filepath.Join(root, KBReadmeFile), []byte(customized), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, err = ReadProfileStatus(root); err != nil {
		t.Fatal(err)
	}
	if status.PromptFile != "" {
		t.Errorf("PromptFile = %q, want no prompt named for a customized README", status.PromptFile)
	}
}
