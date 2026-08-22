package index

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildCreatesFlatDeterministicIndex(t *testing.T) {
	root := t.TempDir()
	active := card("active", "pattern", "active", "# Active\n\nIntro.\n\n## Guidance\n\nUse this.")
	draft := card("draft", "pattern", "draft", "# Draft")
	writeCard(t, root, "patterns/z-active.md", active)
	writeCard(t, root, "patterns/a-draft.md", draft)
	before := time.Now().UTC()

	result, err := Build(root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if result.Index.Version != Version || result.Index.Root != root {
		t.Errorf("index identity = version %d, root %q", result.Index.Version, result.Index.Root)
	}
	if result.Index.GeneratedAt.Before(before) || result.Index.GeneratedAt.After(time.Now().UTC()) {
		t.Errorf("GeneratedAt = %v, want current build time", result.Index.GeneratedAt)
	}
	if len(result.Index.Documents) != 1 || result.Index.Documents[0].ID != "active" {
		t.Fatalf("Documents = %#v, want active only", result.Index.Documents)
	}
	if result.Index.Documents[0].Content != active {
		t.Errorf("document content did not preserve complete Markdown")
	}
	if len(result.Index.Chunks) != 2 || result.Index.Chunks[0].Ordinal != 0 || result.Index.Chunks[1].Ordinal != 1 {
		t.Errorf("Chunks = %#v", result.Index.Chunks)
	}
	if result.Skipped != 1 || len(result.Warnings) != 1 {
		t.Errorf("diagnostics = skipped %d, warnings %#v", result.Skipped, result.Warnings)
	}
	if result.SourceBytes != int64(len(active)+len(draft)) {
		t.Errorf("SourceBytes = %d, want %d", result.SourceBytes, len(active)+len(draft))
	}
	if result.Path != "" {
		t.Errorf("Build Path = %q, want empty until persisted", result.Path)
	}
}

func TestDefaultPathUsesCanonicalRootHash(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "repo")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}

	got, err := DefaultPath(link)
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	digest := sha256.Sum256([]byte(root))
	want := filepath.Join(home, ".local", "share", "wfm-rag", hex.EncodeToString(digest[:]), "index.json")
	if got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestWriteAndLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "dependencies/card.md", card("card", "dependency", "active", "# Card\n\nGuidance."))
	result, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "private", "root-hash")
	path := filepath.Join(directory, "index.json")

	if err := Write(path, result.Index); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	loaded, err := Load(path, root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, result.Index) {
		t.Errorf("round trip mismatch\nloaded: %#v\nbuilt:  %#v", loaded, result.Index)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("index permissions = %o, want 600", got)
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("index directory permissions = %o, want 700", got)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".index-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("temporary files remain: %v", matches)
	}
}

func TestRebuildPreservesValidIndexAfterCorpusFailure(t *testing.T) {
	root := t.TempDir()
	cardPath := writeCard(t, root, "patterns/card.md", card("card", "pattern", "active", "# Original"))
	path := filepath.Join(t.TempDir(), "index.json")
	first, err := Rebuild(root, path)
	if err != nil {
		t.Fatalf("first Rebuild() error = %v", err)
	}
	if first.Path != path {
		t.Errorf("Path = %q, want %q", first.Path, path)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(cardPath, []byte("not frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(root, path); err == nil {
		t.Fatal("Rebuild() error = nil, want malformed corpus error")
	}
	afterFailure, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterFailure, original) {
		t.Error("failed rebuild changed the previous index")
	}

	if err := os.WriteFile(cardPath, []byte(card("card", "pattern", "active", "# Updated")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(root, path); err != nil {
		t.Fatalf("replacement Rebuild() error = %v", err)
	}
	loaded, err := Load(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Documents[0].Title != "Updated" {
		t.Errorf("replacement title = %q, want Updated", loaded.Documents[0].Title)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".index-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("temporary files remain: %v", matches)
	}
}

func TestLoadErrors(t *testing.T) {
	root := t.TempDir()
	otherRoot := t.TempDir()

	t.Run("missing", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "missing.json"), root)
		if !errors.Is(err, ErrMissing) {
			t.Fatalf("Load() error = %v, want ErrMissing", err)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "index.json")
		writeFile(t, path, "{")
		_, err := Load(path, root)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Load() error = %v, want ErrInvalid", err)
		}
	})

	t.Run("trailing json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "index.json")
		writeFile(t, path, `{}`+"\n{}")
		_, err := Load(path, root)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Load() error = %v, want ErrInvalid", err)
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "index.json")
		writeFile(t, path, `{"version":2}`)
		_, err := Load(path, root)
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Fatalf("Load() error = %v, want ErrUnsupportedVersion", err)
		}
	})

	t.Run("root mismatch", func(t *testing.T) {
		writeCard(t, root, "patterns/card.md", card("card", "pattern", "active", "# Card"))
		result, err := Build(root)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "index.json")
		if err := Write(path, result.Index); err != nil {
			t.Fatal(err)
		}
		_, err = Load(path, otherRoot)
		if !errors.Is(err, ErrRootMismatch) {
			t.Fatalf("Load() error = %v, want ErrRootMismatch", err)
		}
	})

	t.Run("invalid root", func(t *testing.T) {
		writeCard(t, root, "patterns/card.md", card("card", "pattern", "active", "# Card"))
		result, err := Build(root)
		if err != nil {
			t.Fatal(err)
		}
		result.Index.Root = "relative"
		encoded, err := json.Marshal(result.Index)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "index.json")
		writeFile(t, path, string(encoded))
		_, err = Load(path, root)
		if !errors.Is(err, ErrInvalid) || errors.Is(err, ErrRootMismatch) {
			t.Fatalf("Load() error = %v, want ErrInvalid only", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "target.json")
		writeFile(t, target, `{}`)
		link := filepath.Join(t.TempDir(), "index.json")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		_, err := Load(link, root)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Load() error = %v, want ErrInvalid", err)
		}
	})
}

func TestLoadRejectsOversizedFileBeforeDecoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	writeFile(t, path, strings.Repeat("x", 11))
	_, err := readFile(path, 10)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "exceeds 10 bytes") {
		t.Fatalf("readFile() error = %v, want size error", err)
	}
}

func TestWriteRejectsInvalidValueWithoutReplacingDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	writeFile(t, path, "previous")
	invalid := &Index{Version: Version, Root: "/root", GeneratedAt: time.Now().UTC()}

	if err := Write(path, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Write() error = %v, want ErrInvalid", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "previous" {
		t.Errorf("destination = %q, want previous", content)
	}
}

func TestWriteOverflowCleansTemporaryAndPreservesDestination(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "patterns/card.md", card("card", "pattern", "active", "# Card"))
	result, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "index.json")
	writeFile(t, path, "previous")

	if err := write(path, result.Index, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("write() error = %v, want ErrInvalid", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "previous" {
		t.Errorf("destination = %q, want previous", content)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".index-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("temporary files remain: %v", matches)
	}
}

func TestValidateRejectsInconsistentStructures(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "patterns/card.md", card("card", "pattern", "active", "# Card"))
	result, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Index)
	}{
		{name: "empty content", mutate: func(value *Index) { value.Documents[0].Content = "" }},
		{name: "long id", mutate: func(value *Index) {
			value.Documents[0].ID = strings.Repeat("a", 257)
			value.Chunks[0].CardID = value.Documents[0].ID
		}},
		{name: "unsupported type", mutate: func(value *Index) {
			value.Documents[0].Type = "unknown"
			value.Chunks[0].CardType = "unknown"
		}},
		{name: "traversal source", mutate: func(value *Index) {
			value.Documents[0].Source = "../card.md"
			value.Chunks[0].Source = "../card.md"
		}},
		{name: "uncategorized source", mutate: func(value *Index) {
			value.Documents[0].Source = ".agents/knowledge/schema.md"
			value.Chunks[0].Source = ".agents/knowledge/schema.md"
		}},
		{name: "chunk mismatch", mutate: func(value *Index) { value.Chunks[0].Title = "Different" }},
		{name: "invalid lines", mutate: func(value *Index) { value.Chunks[0].StartLine = 0 }},
		{name: "ordinal gap", mutate: func(value *Index) { value.Chunks[0].Ordinal = 1 }},
		{name: "missing chunks", mutate: func(value *Index) { value.Chunks = value.Chunks[:0] }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := cloneIndex(t, result.Index)
			test.mutate(value)
			if err := validate(value); !errors.Is(err, ErrInvalid) {
				t.Fatalf("validate() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestLimitedWriterRejectsOverflow(t *testing.T) {
	var destination strings.Builder
	writer := &limitedWriter{writer: &destination, remaining: 3, limit: 3}
	if _, err := writer.Write([]byte("four")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Write() error = %v, want ErrInvalid", err)
	}
	if destination.Len() != 0 {
		t.Errorf("destination length = %d, want 0", destination.Len())
	}
}

func cloneIndex(t *testing.T, value *Index) *Index {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var cloned Index
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatal(err)
	}
	return &cloned
}

func card(id, cardType, status, body string) string {
	return "---\n" +
		"id: " + id + "\n" +
		"type: " + cardType + "\n" +
		"status: " + status + "\n" +
		"scope: [go]\n" +
		"triggers: [test]\n" +
		"packages: []\n" +
		"paths: [src/example.go]\n" +
		"source_confidence: high\n" +
		"last_verified: 2026-08-12\n" +
		"---\n\n" + body + "\n"
}

func writeCard(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, ".agents", "knowledge", filepath.FromSlash(relative))
	writeFile(t, path, content)
	return path
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
