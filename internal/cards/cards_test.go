package cards

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLoadValidCardsDeterministically(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "patterns/z-last.md", completeCard("z-last", "pattern", "active", "# Z Last\n\nIntro.\n\n## Guidance\n\nKeep this.\n\n### Detail\n\nTogether."))
	writeCard(t, root, "dependencies/a-first.md", completeCard("a-first", "dependency", "active", "# A First\n\nIntro.\n\n## Guidance\n\nUse this."))

	corpus, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if corpus.Root != root {
		t.Fatalf("Root = %q, want %q", corpus.Root, root)
	}
	if len(corpus.Cards) != 2 {
		t.Fatalf("len(Cards) = %d, want 2", len(corpus.Cards))
	}
	if got := []string{corpus.Cards[0].ID, corpus.Cards[1].ID}; strings.Join(got, ",") != "a-first,z-last" {
		t.Fatalf("card order = %v, want [a-first z-last]", got)
	}
	card := corpus.Cards[1]
	if card.Title != "Z Last" {
		t.Errorf("Title = %q, want %q", card.Title, "Z Last")
	}
	if card.Source != ".agents/knowledge/patterns/z-last.md" {
		t.Errorf("Source = %q", card.Source)
	}
	if len(card.Chunks) != 2 {
		t.Fatalf("len(Chunks) = %d, want 2", len(card.Chunks))
	}
	if card.Chunks[0].Heading != "Z Last" || card.Chunks[0].Ordinal != 0 || !strings.HasPrefix(card.Chunks[0].Text, "# Z Last") {
		t.Errorf("first chunk = %#v", card.Chunks[0])
	}
	if card.Chunks[1].Heading != "Guidance" || card.Chunks[1].Ordinal != 1 {
		t.Errorf("second chunk heading/ordinal = %q/%d", card.Chunks[1].Heading, card.Chunks[1].Ordinal)
	}
	if !strings.Contains(card.Chunks[1].Text, "### Detail") {
		t.Errorf("lower-level heading was not retained: %q", card.Chunks[1].Text)
	}
	if card.Chunks[0].StartLine != 13 || card.Chunks[0].EndLine != 15 {
		t.Errorf("intro lines = %d-%d, want 13-15", card.Chunks[0].StartLine, card.Chunks[0].EndLine)
	}
	if len(corpus.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", corpus.Warnings)
	}
	wantBytes := int64(len(completeCard("z-last", "pattern", "active", "# Z Last\n\nIntro.\n\n## Guidance\n\nKeep this.\n\n### Detail\n\nTogether.")) + len(completeCard("a-first", "dependency", "active", "# A First\n\nIntro.\n\n## Guidance\n\nUse this.")))
	if corpus.SourceBytes != wantBytes {
		t.Errorf("SourceBytes = %d, want %d", corpus.SourceBytes, wantBytes)
	}
}

func TestLoadSkipsInactiveCardsAndWarnsForOmittedMetadata(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "patterns/active.md", completeCard("active", "pattern", "active", "# Active"))
	writeCard(t, root, "patterns/draft.md", minimalCard("draft", "pattern", "draft", "# Draft"))
	writeCard(t, root, "patterns/retired.md", completeCard("retired", "pattern", "retired", "# Retired"))

	corpus, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(corpus.Cards) != 1 || corpus.Cards[0].ID != "active" {
		t.Fatalf("Cards = %v, want active only", corpus.Cards)
	}
	if corpus.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", corpus.Skipped)
	}
	if len(corpus.Warnings) != 3 {
		t.Fatalf("Warnings = %v, want three", corpus.Warnings)
	}
	if corpus.Warnings[0].CardID != "draft" || corpus.Warnings[0].Message != "optional metadata omitted: scope, triggers, packages, paths, source_confidence, last_verified" {
		t.Errorf("first warning = %#v", corpus.Warnings[0])
	}
	if corpus.Warnings[1].Message != "skipped card with status draft" || corpus.Warnings[2].Message != "skipped card with status retired" {
		t.Errorf("skip warnings = %#v", corpus.Warnings[1:])
	}
}

func TestLoadRejectsMalformedCards(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "no frontmatter", content: "# Title\n", want: "missing YAML frontmatter opening delimiter"},
		{name: "unclosed frontmatter", content: "---\nid: broken\n", want: "missing YAML frontmatter closing delimiter"},
		{name: "invalid yaml", content: "---\nid: [\n---\n# Title\n", want: "frontmatter"},
		{name: "not mapping", content: "---\n- value\n---\n# Title\n", want: "expected a mapping"},
		{name: "required field", content: "---\nid: card\ntype: pattern\n---\n# Title\n", want: "id, type, and status are required"},
		{name: "type", content: minimalCard("card", "unknown", "active", "# Title"), want: "unsupported type"},
		{name: "status", content: minimalCard("card", "pattern", "unknown", "# Title"), want: "unsupported status"},
		{name: "long id", content: minimalCard(strings.Repeat("a", MaxIDBytes+1), "pattern", "active", "# Title"), want: "id must be valid UTF-8"},
		{name: "title", content: minimalCard("card", "pattern", "active", "## Guidance\n\nNo title."), want: "level-one heading is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeCard(t, root, "patterns/card.md", test.content)
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want containing %q", err, test.want)
			}
			if !errors.Is(err, ErrMalformedCard) {
				t.Errorf("Load() error = %v, want ErrMalformedCard", err)
			}
		})
	}
}

func TestLoadRejectsDuplicateIDsAcrossStatuses(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "dependencies/first.md", completeCard("same-id", "dependency", "active", "# First"))
	writeCard(t, root, "patterns/second.md", completeCard("same-id", "pattern", "retired", "# Second"))

	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), `duplicate id "same-id"`) {
		t.Fatalf("Load() error = %v, want duplicate ID error", err)
	}
	if !errors.Is(err, ErrDuplicateID) {
		t.Errorf("Load() error = %v, want ErrDuplicateID", err)
	}
}

func TestLoadOnlyReadsCategorizedRegularMarkdown(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "patterns/kept.md", completeCard("kept", "pattern", "active", "# Kept"))
	writeCard(t, root, "patterns/README.md", "not a card")
	writeCard(t, root, "patterns/notes.txt", "not a card")
	writeCard(t, root, "uncategorized/ignored.md", "not a card")
	writeFile(t, filepath.Join(root, ".agents", "knowledge", "schema.md"), "not a card")
	if err := os.MkdirAll(filepath.Join(root, ".agents", "knowledge", "patterns", "directory.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.md")
	writeFile(t, outside, minimalCard("outside", "pattern", "active", "# Outside"))
	if err := os.Symlink(outside, filepath.Join(root, ".agents", "knowledge", "patterns", "linked.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, ".agents", "knowledge", "patterns", "linked-dir")); err != nil {
		t.Fatal(err)
	}

	corpus, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(corpus.Cards) != 1 || corpus.Cards[0].ID != "kept" {
		t.Fatalf("Cards = %v, want kept only", corpus.Cards)
	}
}

func TestLoadEnforcesConfiguredBounds(t *testing.T) {
	t.Run("file bytes", func(t *testing.T) {
		root := t.TempDir()
		writeCard(t, root, "patterns/card.md", completeCard("card", "pattern", "active", "# Card"))
		_, err := load(root, limits{fileBytes: 10, corpusBytes: 1 << 20, cards: 10})
		if err == nil || !strings.Contains(err.Error(), "file size") {
			t.Fatalf("load() error = %v, want file size error", err)
		}
	})

	t.Run("corpus bytes", func(t *testing.T) {
		root := t.TempDir()
		first := completeCard("first", "pattern", "active", "# First")
		writeCard(t, root, "patterns/first.md", first)
		writeCard(t, root, "patterns/second.md", completeCard("second", "pattern", "active", "# Second"))
		_, err := load(root, limits{fileBytes: 1 << 20, corpusBytes: int64(len(first) + 1), cards: 10})
		if err == nil || !strings.Contains(err.Error(), "source Markdown") {
			t.Fatalf("load() error = %v, want corpus size error", err)
		}
	})

	t.Run("card count", func(t *testing.T) {
		root := t.TempDir()
		writeCard(t, root, "patterns/first.md", completeCard("first", "pattern", "active", "# First"))
		writeCard(t, root, "patterns/second.md", completeCard("second", "pattern", "active", "# Second"))
		_, err := load(root, limits{fileBytes: 1 << 20, corpusBytes: 1 << 20, cards: 1})
		if err == nil || !strings.Contains(err.Error(), "card count") {
			t.Fatalf("load() error = %v, want card count error", err)
		}
	})
}

func TestLoadSplitsOversizedSections(t *testing.T) {
	root := t.TempDir()
	first := strings.Repeat("a", 5_000)
	second := strings.Repeat("b", 4_000)
	longParagraph := strings.Repeat("界", MaxChunkChars+1)
	body := "# Big Card\n\n## Paragraphs\n\n" + first + "\n\n" + second + "\n\n## Hard Split\n\n" + longParagraph
	writeCard(t, root, "patterns/big.md", completeCard("big", "pattern", "active", body))

	corpus, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	chunks := corpus.Cards[0].Chunks
	if len(chunks) != 6 {
		t.Fatalf("len(Chunks) = %d, want 6", len(chunks))
	}
	for _, chunk := range chunks {
		if got := utf8.RuneCountInString(chunk.Text); got > MaxChunkChars {
			t.Errorf("chunk %d has %d characters", chunk.Ordinal, got)
		}
	}
	if chunks[1].Heading != "Paragraphs" || !strings.Contains(chunks[1].Text, first) {
		t.Errorf("first paragraph chunk = heading %q, len %d", chunks[1].Heading, len(chunks[1].Text))
	}
	if chunks[3].Heading != "Hard Split" || chunks[4].Heading != "Hard Split" || chunks[5].Heading != "Hard Split" {
		t.Errorf("hard-split headings = %q, %q, %q", chunks[3].Heading, chunks[4].Heading, chunks[5].Heading)
	}
}

func TestCanonicalRootResolvesSymlink(t *testing.T) {
	root := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "repository")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalRoot(link)
	if err != nil {
		t.Fatalf("CanonicalRoot() error = %v", err)
	}
	if got != root {
		t.Errorf("CanonicalRoot() = %q, want %q", got, root)
	}
}

func TestLoadRejectsSymlinkedAgentDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeCard(t, outside, "patterns/outside.md", completeCard("outside", "pattern", "active", "# Outside"))
	if err := os.Symlink(filepath.Join(outside, ".agents"), filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}

	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "not a regular directory") {
		t.Fatalf("Load() error = %v, want symlinked agent directory error", err)
	}
}

func completeCard(id, cardType, status, body string) string {
	return "---\n" +
		"id: " + id + "\n" +
		"type: " + cardType + "\n" +
		"status: " + status + "\n" +
		"scope: []\n" +
		"triggers: []\n" +
		"packages: []\n" +
		"paths: []\n" +
		"source_confidence: high\n" +
		"last_verified: 2026-08-12\n" +
		"---\n\n" + body + "\n"
}

func minimalCard(id, cardType, status, body string) string {
	return "---\n" +
		"id: " + id + "\n" +
		"type: " + cardType + "\n" +
		"status: " + status + "\n" +
		"---\n\n" + body + "\n"
}

func writeCard(t *testing.T, root, relative, content string) {
	t.Helper()
	writeFile(t, filepath.Join(root, ".agents", "knowledge", filepath.FromSlash(relative)), content)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
