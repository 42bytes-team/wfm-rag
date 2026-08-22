package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	indexpkg "wfm-rag/internal/index"
	"wfm-rag/internal/search"
)

func TestRunIndexSearchAndGetJSON(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "index.json")
	content := testCard("ini-list-parsing", "dependency", "active", "# INI List Parsing\n\n## Guidance\n\nUse Key.Strings for comma separated config.")
	writeTestCard(t, root, "dependencies/ini.md", content)
	writeTestCard(t, root, "patterns/draft.md", testCard("draft", "pattern", "draft", "# Draft"))

	stdout, stderr, code := execute(t, "index", "--root", root, "--index", indexPath, "--format", "json")
	if code != 0 || stderr != "" {
		t.Fatalf("index code = %d, stderr = %q", code, stderr)
	}
	var indexed indexOutput
	if err := json.Unmarshal([]byte(stdout), &indexed); err != nil {
		t.Fatalf("decode index output: %v\n%s", err, stdout)
	}
	if indexed.Cards != 1 || indexed.Skipped != 1 || indexed.Chunks != 2 || indexed.Index != indexPath {
		t.Errorf("index output = %#v", indexed)
	}
	if len(indexed.Warnings) != 1 || indexed.Warnings[0].CardID != "draft" {
		t.Errorf("warnings = %#v", indexed.Warnings)
	}

	stdout, stderr, code = execute(t, "search", "--root", root, "--index", indexPath, "--format=json", "--package", "gopkg.in/ini.v1", "--path", "src/config/ini.go", "--type", "dependency", "config parser comma separated")
	if code != 0 || stderr != "" {
		t.Fatalf("search code = %d, stderr = %q", code, stderr)
	}
	var searched search.Response
	if err := json.Unmarshal([]byte(stdout), &searched); err != nil {
		t.Fatalf("decode search output: %v\n%s", err, stdout)
	}
	if searched.Root != root || searched.Query != "config parser comma separated" || len(searched.Results) != 1 {
		t.Fatalf("search response = %#v", searched)
	}
	if searched.Results[0].ID != "ini-list-parsing" || searched.Results[0].Score == 0 {
		t.Errorf("search result = %#v", searched.Results[0])
	}

	stdout, stderr, code = execute(t, "get", "--root", root, "--index", indexPath, "--format", "json", "ini-list-parsing")
	if code != 0 || stderr != "" {
		t.Fatalf("get code = %d, stderr = %q", code, stderr)
	}
	var got search.GetResponse
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode get output: %v\n%s", err, stdout)
	}
	if got.Card.ID != "ini-list-parsing" || got.Card.Content != content {
		t.Errorf("get response = %#v", got)
	}
}

func TestRunTextOutput(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "index.json")
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\n## Guidance\n\nNeedle guidance."))

	stdout, stderr, code := execute(t, "index", "--root", root, "--index", indexPath)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Indexed 1 cards and 2 chunks") || !strings.Contains(stdout, "Index: "+indexPath) {
		t.Fatalf("index output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "search", "--root", root, "--index", indexPath, "needle")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Indexed: ") || !strings.Contains(stdout, "1. Card [card]") || !strings.Contains(stdout, "Needle guidance") {
		t.Fatalf("search output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "search", "--root", root, "--index", indexPath, "absent")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Indexed: ") || !strings.HasSuffix(stdout, "No results.\n") {
		t.Fatalf("empty output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "get", "--root", root, "--index", indexPath, "card")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Source: .agents/knowledge/patterns/card.md") || !strings.Contains(stdout, "# Card") {
		t.Fatalf("get output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRunDefaultIndexPathRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))

	stdout, stderr, code := execute(t, "index", "--root", link, "--format=json")
	if code != 0 || stderr != "" {
		t.Fatalf("index code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var indexed indexOutput
	if err := json.Unmarshal([]byte(stdout), &indexed); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(indexed.Index, filepath.Join(home, ".local", "share", "wfm-rag")) {
		t.Errorf("default index = %q", indexed.Index)
	}
	if _, err := os.Stat(indexed.Index); err != nil {
		t.Fatalf("default index not written: %v", err)
	}
	stdout, stderr, code = execute(t, "search", "--root", link, "--format=json", "needle")
	if code != 0 || stderr != "" {
		t.Fatalf("search code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var response search.Response
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].ID != "card" {
		t.Errorf("search response = %#v", response)
	}
}

func TestRunCleansCustomIndexPath(t *testing.T) {
	root := t.TempDir()
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card"))
	directory := t.TempDir()
	unclean := filepath.Join(directory, "missing", "..", "index.json")
	clean := filepath.Join(directory, "index.json")
	stdout, stderr, code := execute(t, "index", "--root", root, "--index", unclean, "--format=json")
	if code != 0 || stderr != "" {
		t.Fatalf("index code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output indexOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.Index != clean {
		t.Errorf("reported index = %q, want %q", output.Index, clean)
	}
}

func TestRunDoesNotTreatPositionalFormatAsErrorFormat(t *testing.T) {
	root := t.TempDir()
	stdout, stderr, code := execute(t, "search", "--root", root, "--", "--format=json")
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "error: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q, want text index error", code, stdout, stderr)
	}
}

func TestRunArgumentErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no command", want: "a command is required"},
		{name: "unknown command", args: []string{"unknown"}, want: `unknown command "unknown"`},
		{name: "root required", args: []string{"index"}, want: "--root is required"},
		{name: "extra index argument", args: []string{"index", "--root", "/tmp", "extra"}, want: "expected 0 positional argument"},
		{name: "missing query", args: []string{"search", "--root", "/tmp"}, want: "expected 1 positional argument"},
		{name: "extra get id", args: []string{"get", "--root", "/tmp", "a", "b"}, want: "expected 1 positional argument"},
		{name: "mcp root required", args: []string{"mcp"}, want: "--root is required"},
		{name: "extra mcp argument", args: []string{"mcp", "--root", "/tmp", "extra"}, want: "expected 0 positional argument"},
		{name: "mcp format rejected", args: []string{"mcp", "--root", "/tmp", "--format=json"}, want: "flag provided but not defined"},
		{name: "unknown flag", args: []string{"index", "--unknown"}, want: "flag provided but not defined"},
		{name: "bad format", args: []string{"index", "--root", "/tmp", "--format", "xml"}, want: "unsupported format"},
		{name: "zero limit", args: []string{"search", "--root", "/tmp", "--limit", "0", "query"}, want: "limit must be between"},
		{name: "bad limit syntax", args: []string{"search", "--root", "/tmp", "--limit", "2x", "query"}, want: "invalid integer"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := execute(t, test.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, test.want) || !strings.Contains(stderr, "usage:") {
				t.Fatalf("run() = code %d, stdout %q, stderr %q; want argument error containing %q", code, stdout, stderr, test.want)
			}
		})
	}
}

func TestRunClassifiedJSONErrors(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "index.json")
	tests := []struct {
		name     string
		args     []string
		wantCode int
		want     string
	}{
		{name: "invalid arguments", args: []string{"search", "--format=json", "--root", root, "--index", indexPath, "--limit", "0", "query"}, wantCode: 2, want: "invalid_arguments"},
		{name: "missing index", args: []string{"search", "--format=json", "--root", root, "--index", indexPath, "query"}, wantCode: 1, want: "index_missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := execute(t, test.args...)
			if code != test.wantCode || stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var output errorOutput
			if err := json.Unmarshal([]byte(stderr), &output); err != nil {
				t.Fatalf("decode error JSON: %v\n%s", err, stderr)
			}
			if output.Error.Code != test.want || strings.Contains(stderr, "usage:") {
				t.Errorf("error output = %#v", output)
			}
		})
	}
}

func TestRunClassifiesStoredIndexErrors(t *testing.T) {
	root := t.TempDir()
	otherRoot := t.TempDir()
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card"))
	result, err := indexpkg.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*indexpkg.Index)
		want   string
	}{
		{name: "unsupported", mutate: func(value *indexpkg.Index) { value.Version = 2 }, want: "unsupported_index"},
		{name: "root mismatch", mutate: func(value *indexpkg.Index) { value.Root = otherRoot }, want: "index_root_mismatch"},
		{name: "invalid", mutate: func(value *indexpkg.Index) { value.Documents[0].Title = "" }, want: "invalid_index"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := cloneCLIIndex(t, result.Index)
			test.mutate(value)
			path := filepath.Join(t.TempDir(), "index.json")
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := execute(t, "search", "--format=json", "--root", root, "--index", path, "query")
			assertJSONError(t, stdout, stderr, code, 1, test.want)
		})
	}
}

func TestRunClassifiesCorpusAndGetErrors(t *testing.T) {
	t.Run("duplicate card", func(t *testing.T) {
		root := t.TempDir()
		writeTestCard(t, root, "patterns/first.md", testCard("same", "pattern", "active", "# First"))
		writeTestCard(t, root, "dependencies/second.md", testCard("same", "dependency", "active", "# Second"))
		stdout, stderr, code := execute(t, "index", "--root", root, "--index", filepath.Join(t.TempDir(), "index.json"), "--format=json")
		assertJSONError(t, stdout, stderr, code, 1, "duplicate_card_id")
	})

	t.Run("malformed corpus", func(t *testing.T) {
		root := t.TempDir()
		writeTestCard(t, root, "patterns/card.md", "not a card")
		stdout, stderr, code := execute(t, "index", "--root", root, "--index", filepath.Join(t.TempDir(), "index.json"), "--format=json")
		assertJSONError(t, stdout, stderr, code, 1, "malformed_card_corpus")
	})

	t.Run("card not found", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(t.TempDir(), "index.json")
		writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card"))
		if _, _, code := execute(t, "index", "--root", root, "--index", path); code != 0 {
			t.Fatal("index setup failed")
		}
		stdout, stderr, code := execute(t, "get", "--root", root, "--index", path, "--format=json", "missing")
		assertJSONError(t, stdout, stderr, code, 1, "card_not_found")
	})
}

func TestRunHelp(t *testing.T) {
	stdout, stderr, code := execute(t, "help")
	if code != 0 || stderr != "" || stdout != usage+"\n" {
		t.Errorf("help = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestCLIProcessKeepsErrorsOnStderr(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--", "search", "--format=json")
	command.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	stdout, err := command.Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("process error = %v, want ExitError", err)
	}
	if exitErr.ExitCode() != 2 || len(stdout) != 0 {
		t.Errorf("exit = %d, stdout = %q", exitErr.ExitCode(), stdout)
	}
	var output errorOutput
	if err := json.Unmarshal(exitErr.Stderr, &output); err != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", err, exitErr.Stderr)
	}
	if output.Error.Code != "invalid_arguments" {
		t.Errorf("error code = %q", output.Error.Code)
	}
}

func TestCLIProcessKeepsOperationalErrorsOnStderr(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.json")
	command := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--", "search", "--root", root, "--index", missing, "--format=json", "query")
	command.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	stdout, err := command.Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("process error = %v, want ExitError", err)
	}
	if exitErr.ExitCode() != 1 || len(stdout) != 0 {
		t.Errorf("exit = %d, stdout = %q", exitErr.ExitCode(), stdout)
	}
	var output errorOutput
	if err := json.Unmarshal(exitErr.Stderr, &output); err != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", err, exitErr.Stderr)
	}
	if output.Error.Code != "index_missing" {
		t.Errorf("error code = %q", output.Error.Code)
	}
}

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index + 1
			break
		}
	}
	os.Exit(run(os.Args[separator:], os.Stdout, os.Stderr))
}

func execute(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func assertJSONError(t *testing.T, stdout, stderr string, code, wantCode int, want string) {
	t.Helper()
	if code != wantCode || stdout != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output errorOutput
	if err := json.Unmarshal([]byte(stderr), &output); err != nil {
		t.Fatalf("decode error JSON: %v\n%s", err, stderr)
	}
	if output.Error.Code != want {
		t.Errorf("error code = %q, want %q", output.Error.Code, want)
	}
}

func testCard(id, cardType, status, body string) string {
	return "---\n" +
		"id: " + id + "\n" +
		"type: " + cardType + "\n" +
		"status: " + status + "\n" +
		"scope: [config]\n" +
		"triggers: [parser, comma-separated]\n" +
		"packages: [gopkg.in/ini.v1]\n" +
		"paths: [src/config]\n" +
		"source_confidence: high\n" +
		"last_verified: 2026-08-12\n" +
		"---\n\n" + body + "\n"
}

func writeTestCard(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, ".agents", "knowledge", filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func cloneCLIIndex(t *testing.T, value *indexpkg.Index) *indexpkg.Index {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var cloned indexpkg.Index
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatal(err)
	}
	return &cloned
}
