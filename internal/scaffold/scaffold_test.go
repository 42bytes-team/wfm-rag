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
		filepath.Join(knowledgeDir, "schema.md"),
		skillFile,
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
	skill, err := os.ReadFile(filepath.Join(root, skillFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(skill), "{{PROJECT}}") || !strings.Contains(string(skill), "--project api") {
		t.Error("starter skill must render the project ID into every command")
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
