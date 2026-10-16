package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	indexpkg "wfm-rag/internal/index"
	"wfm-rag/internal/project"
	"wfm-rag/internal/search"
)

func TestRunIndexSearchGetJSON(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "index.json")
	content := testCard("ini-list-parsing", "dependency", "active", "# INI List Parsing\n\n## Guidance\n\nUse Key.Strings for comma separated config.")
	writeTestCard(t, root, "dependencies/ini.md", content)
	writeTestCard(t, root, "patterns/draft.md", testCard("draft", "pattern", "draft", "# Draft"))
	registerProject(t, "api", root)

	stdout, stderr, code := execute(t, "index", "--project", "api", "--index", indexPath, "--format", "json")
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

	stdout, stderr, code = execute(t, "search", "--project", "api", "--index", indexPath, "--format=json", "--package", "gopkg.in/ini.v1", "--path", "src/config/ini.go", "--type", "dependency", "config parser comma separated")
	if code != 0 || stderr != "" {
		t.Fatalf("search code = %d stderr = %q", code, stderr)
	}
	var searched search.Response
	if err := json.Unmarshal([]byte(stdout), &searched); err != nil {
		t.Fatalf("decode search output: %v\n%s", err, stdout)
	}
	if searched.Project != "api" || searched.Root != root || searched.Query != "config parser comma separated" || len(searched.Results) != 1 {
		t.Fatalf("search response = %#v", searched)
	}
	if searched.IndexedAt == "" {
		t.Errorf("search response lacks indexed_at: %#v", searched)
	}
	if searched.Results[0].ID != "ini-list-parsing" || searched.Results[0].Score == 0 {
		t.Errorf("search result = %#v", searched.Results[0])
	}

	stdout, stderr, code = execute(t, "get", "--project", "api", "--index", indexPath, "--format", "json", "ini-list-parsing")
	if code != 0 || stderr != "" {
		t.Fatalf("get code = %d, stderr = %q", code, stderr)
	}
	var got search.GetResponse
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode get output: %v\n%s", err, stdout)
	}
	if got.Project != "api" || got.Root != root || got.Card.ID != "ini-list-parsing" || got.Card.Content != content {
		t.Errorf("get response = %#v", got)
	}
}

func TestRunTextOutput(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "index.json")
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\n## Guidance\n\nNeedle guidance."))
	registerProject(t, "api", root)

	stdout, stderr, code := execute(t, "index", "--project", "api", "--index", indexPath)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Indexed 1 cards and 2 chunks") || !strings.Contains(stdout, "Index: "+indexPath) {
		t.Fatalf("index output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "search", "--project", "api", "--index", indexPath, "needle")
	if code != 0 || stderr != "" ||
		!strings.Contains(stdout, "Project: api\n") ||
		!strings.Contains(stdout, "Indexed: ") ||
		!strings.Contains(stdout, "1. Card [card]") ||
		!strings.Contains(stdout, "Needle guidance") {
		t.Fatalf("search output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.Contains(stdout, "Root:") {
		t.Errorf("search output repeats the root per result: %q", stdout)
	}
	stdout, stderr, code = execute(t, "search", "--project", "api", "--index", indexPath, "absent")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Project: api\n") || !strings.HasSuffix(stdout, "No results.\n") {
		t.Fatalf("empty output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "get", "--project", "api", "--index", indexPath, "card")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Source: .agents/knowledge/patterns/card.md") || !strings.Contains(stdout, "# Card") {
		t.Fatalf("get output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRunDefaultIndexPathRoundTrip(t *testing.T) {
	home := isolateState(t)
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
	registerProject(t, "api", link)

	stdout, stderr, code := execute(t, "index", "--project", "api", "--format=json")
	if code != 0 || stderr != "" {
		t.Fatalf("index code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var indexed indexOutput
	if err := json.Unmarshal([]byte(stdout), &indexed); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(root))
	want := filepath.Join(home, ".local", "share", "wfm-rag", hex.EncodeToString(digest[:]), "index.json")
	if indexed.Root != root || indexed.Index != want {
		t.Errorf("index output root=%q index=%q, want canonical root with %q", indexed.Root, indexed.Index, want)
	}
	if _, err := os.Stat(indexed.Index); err != nil {
		t.Fatalf("default index not written: %v", err)
	}
	stdout, stderr, code = execute(t, "search", "--project", "api", "--format=json", "needle")
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

func TestRunXDGDataHomeStateDir(t *testing.T) {
	isolateState(t)
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	root := t.TempDir()
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
	registerProject(t, "api", root)

	stdout, stderr, code := execute(t, "index", "--project", "api", "--format=json")
	if code != 0 || stderr != "" {
		t.Fatalf("index code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var indexed indexOutput
	if err := json.Unmarshal([]byte(stdout), &indexed); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(root))
	want := filepath.Join(xdg, "wfm-rag", hex.EncodeToString(digest[:]), "index.json")
	if indexed.Index != want {
		t.Errorf("index = %q, want %q", indexed.Index, want)
	}
	if _, err := os.Stat(filepath.Join(xdg, "wfm-rag", "projects.json")); err != nil {
		t.Errorf("registry not written under XDG_DATA_HOME: %v", err)
	}
	stdout, stderr, code = execute(t, "search", "--project", "api", "--format=json", "needle")
	if code != 0 || stderr != "" {
		t.Fatalf("search code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRunCleansCustomIndexPath(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card"))
	registerProject(t, "api", root)
	directory := t.TempDir()
	unclean := filepath.Join(directory, "missing", "..", "index.json")
	clean := filepath.Join(directory, "index.json")
	stdout, stderr, code := execute(t, "index", "--project", "api", "--index", unclean, "--format=json")
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

func TestProjectInitRebindRefresh(t *testing.T) {
	isolateState(t)
	first := t.TempDir()
	second := t.TempDir()
	third := t.TempDir()

	stdout, stderr, code := execute(t, "project", "init", "api", "--dir", first)
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "Registered api -> "+first+"\n") ||
		!strings.Contains(stdout, "Scaffolded 10 knowledge-base files:") {
		t.Fatalf("first init: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "project", "init", "api", "--dir", first)
	if code != 0 || stderr != "" || stdout != "Refreshed api -> "+first+"\n" {
		t.Fatalf("refresh: code=%d stdout=%q stderr=%q; a completed skeleton must not be re-scaffolded", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "project", "init", "api", "--dir", second)
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "Rebound api: "+first+" -> "+second+"\n") ||
		!strings.Contains(stdout, "Scaffolded 10 knowledge-base files:") {
		t.Fatalf("rebind: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	stdout, stderr, code = execute(t, "project", "init", "api", "--dir", third, "--format", "json")
	if code != 0 || stderr != "" {
		t.Fatalf("json init: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var result projectMutationOutput
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Project != "api" || result.Root != third || result.PreviousRoot != second || !result.Rebound || result.Created {
		t.Errorf("init result = %#v, want rebind from %q to %q", result, second, third)
	}
	if len(result.Scaffolded) != 10 {
		t.Errorf("init scaffolded %v, want 10 skeleton files in a fresh directory", result.Scaffolded)
	}

	writeTestCard(t, third, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
	stdout, stderr, code = execute(t, "index", "--project", "api", "--format=json")
	if code != 0 || stderr != "" {
		t.Fatalf("index after rebind: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var indexed indexOutput
	if err := json.Unmarshal([]byte(stdout), &indexed); err != nil {
		t.Fatal(err)
	}
	if indexed.Root != third {
		t.Errorf("index built from %q, want rebound root %q", indexed.Root, third)
	}
}

func TestProjectRemoveAndEmptyList(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	registerProject(t, "api", root)

	stdout, stderr, code := execute(t, "project", "remove", "api")
	if code != 0 || stderr != "" || stdout != "Removed api ("+root+")\n" {
		t.Fatalf("remove: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "project", "list")
	if code != 0 || stderr != "" || stdout != "No projects registered.\n" {
		t.Fatalf("list: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "project", "remove", "api", "--format", "json")
	assertJSONError(t, stdout, stderr, code, 2, "project_unknown")
	stdout, stderr, code = execute(t, "project", "remove", "Bad_ID", "--format", "json")
	assertJSONError(t, stdout, stderr, code, 2, "project_id_invalid")
}

func TestProjectListStatuses(t *testing.T) {
	isolateState(t)
	apiRoot := t.TempDir()
	webRoot := t.TempDir()
	idleRoot := t.TempDir()
	ghostRoot := t.TempDir()
	for _, setup := range []struct {
		id   string
		root string
	}{{"api", apiRoot}, {"web", webRoot}, {"idle", idleRoot}, {"ghost", ghostRoot}} {
		registerProject(t, setup.id, setup.root)
	}
	writeTestCard(t, apiRoot, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
	if _, stderr, code := execute(t, "index", "--project", "api"); code != 0 || stderr != "" {
		t.Fatalf("index setup: code=%d stderr=%q", code, stderr)
	}
	if err := os.RemoveAll(ghostRoot); err != nil {
		t.Fatal(err)
	}
	// A structurally invalid index file must not affect list: it never opens index files.
	webIndex := derivedIndexPath(t, webRoot)
	if err := os.MkdirAll(filepath.Dir(webIndex), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(webIndex, []byte("not an index"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := execute(t, "project", "list")
	if code != 0 || stderr != "" {
		t.Fatalf("list: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("list lines = %q, want one per project", lines)
	}
	if !strings.HasPrefix(lines[0], "api "+apiRoot+" ") || strings.Contains(lines[0], "missing") {
		t.Errorf("api line = %q, want healthy status", lines[0])
	}
	if !strings.HasPrefix(lines[1], "ghost "+ghostRoot+" ") || !strings.Contains(lines[1], "[root missing]") || !strings.Contains(lines[1], "[index missing]") {
		t.Errorf("ghost line = %q, want root and index missing", lines[1])
	}
	if !strings.HasPrefix(lines[2], "idle "+idleRoot+" ") || !strings.Contains(lines[2], "[index missing]") || strings.Contains(lines[2], "[root missing]") {
		t.Errorf("idle line = %q, want index missing only", lines[2])
	}
	if !strings.HasPrefix(lines[3], "web "+webRoot+" ") || strings.Contains(lines[3], "missing") {
		t.Errorf("web line = %q, want present despite unreadable index content", lines[3])
	}

	stdout, stderr, code = execute(t, "project", "list", "--format", "json")
	if code != 0 || stderr != "" {
		t.Fatalf("json list: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output projectsOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Projects) != 4 {
		t.Fatalf("projects = %#v", output.Projects)
	}
	wantFlags := []project.Status{
		{ID: "api", Root: apiRoot, IndexPath: derivedIndexPath(t, apiRoot)},
		{ID: "ghost", Root: ghostRoot, IndexPath: derivedIndexPath(t, ghostRoot), RootMissing: true, IndexMissing: true},
		{ID: "idle", Root: idleRoot, IndexPath: derivedIndexPath(t, idleRoot), IndexMissing: true},
		{ID: "web", Root: webRoot, IndexPath: webIndex},
	}
	for i, want := range wantFlags {
		got := output.Projects[i]
		if got.ID != want.ID || got.Root != want.Root || got.IndexPath != want.IndexPath ||
			got.RootMissing != want.RootMissing || got.IndexMissing != want.IndexMissing {
			t.Errorf("status[%d] = %#v, want %#v", i, got, want)
		}
	}
}

func TestProjectHealthcheck(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		isolateState(t)
		root := t.TempDir()
		registerProject(t, "api", root)
		writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
		if _, stderr, code := execute(t, "index", "--project", "api"); code != 0 || stderr != "" {
			t.Fatalf("index setup: code=%d stderr=%q", code, stderr)
		}
		stdout, stderr, code := execute(t, "project", "healthcheck")
		if code != 0 || stderr != "" || stdout != "No problems found.\n" {
			t.Fatalf("healthcheck: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("dead root", func(t *testing.T) {
		isolateState(t)
		root := t.TempDir()
		registerProject(t, "ghost", root)
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		stdout, _, code := execute(t, "project", "healthcheck")
		if code != 1 || !strings.Contains(stdout, "root_missing ghost "+root) {
			t.Fatalf("healthcheck: code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("missing index", func(t *testing.T) {
		isolateState(t)
		registerProject(t, "web", t.TempDir())
		stdout, _, code := execute(t, "project", "healthcheck")
		if code != 1 || !strings.Contains(stdout, "index_missing web ") {
			t.Fatalf("healthcheck: code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("verify reports corrupt index", func(t *testing.T) {
		isolateState(t)
		root := t.TempDir()
		registerProject(t, "api", root)
		writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
		if _, stderr, code := execute(t, "index", "--project", "api"); code != 0 || stderr != "" {
			t.Fatalf("index setup: code=%d stderr=%q", code, stderr)
		}
		if err := os.WriteFile(derivedIndexPath(t, root), []byte("not an index"), 0o600); err != nil {
			t.Fatal(err)
		}
		if stdout, _, code := execute(t, "project", "healthcheck"); code != 0 {
			t.Fatalf("presence-only healthcheck should pass a present unreadable file: code=%d stdout=%q", code, stdout)
		}
		stdout, _, code := execute(t, "project", "healthcheck", "--verify")
		if code != 1 || !strings.Contains(stdout, "index_invalid api ") {
			t.Fatalf("verify healthcheck: code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("dangling index directory", func(t *testing.T) {
		isolateState(t)
		root := t.TempDir()
		registerProject(t, "api", root)
		writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
		if _, stderr, code := execute(t, "index", "--project", "api"); code != 0 || stderr != "" {
			t.Fatalf("index setup: code=%d stderr=%q", code, stderr)
		}
		if _, _, code := execute(t, "project", "remove", "api"); code != 0 {
			t.Fatal("remove setup failed")
		}
		dangling := filepath.Dir(derivedIndexPath(t, root))
		stdout, _, code := execute(t, "project", "healthcheck")
		if code != 1 || !strings.Contains(stdout, "index_dangling "+dangling) {
			t.Fatalf("healthcheck: code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("registry missing is a finding", func(t *testing.T) {
		isolateState(t)
		stdout, _, code := execute(t, "project", "healthcheck")
		if code != 1 || !strings.Contains(stdout, "project_registry_missing ") {
			t.Fatalf("healthcheck: code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("json findings", func(t *testing.T) {
		isolateState(t)
		root := t.TempDir()
		registerProject(t, "web", root)
		stdout, stderr, code := execute(t, "project", "healthcheck", "--format", "json")
		if code != 1 || stdout == "" {
			t.Fatalf("healthcheck: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var output healthcheckOutput
		if err := json.Unmarshal([]byte(stdout), &output); err != nil {
			t.Fatal(err)
		}
		if len(output.Findings) != 1 || output.Findings[0].Kind != "index_missing" || output.Findings[0].Project != "web" {
			t.Errorf("findings = %#v", output.Findings)
		}
		if len(output.Purged) != 0 {
			t.Errorf("purged = %#v", output.Purged)
		}
	})
}

func TestProjectHealthcheckPurge(t *testing.T) {
	t.Run("purges clean dangling only", func(t *testing.T) {
		isolateState(t)
		danglingRoot := t.TempDir()
		claimedRoot := t.TempDir()
		registerProject(t, "gone", danglingRoot)
		registerProject(t, "api", claimedRoot)
		writeTestCard(t, danglingRoot, "patterns/card.md", testCard("gone-card", "pattern", "active", "# Gone\n\nNeedle."))
		writeTestCard(t, claimedRoot, "patterns/card.md", testCard("api-card", "pattern", "active", "# Api\n\nNeedle."))
		if _, stderr, code := execute(t, "index", "--project", "gone"); code != 0 || stderr != "" {
			t.Fatalf("index setup: code=%d stderr=%q", code, stderr)
		}
		if _, stderr, code := execute(t, "index", "--project", "api"); code != 0 || stderr != "" {
			t.Fatalf("index setup: code=%d stderr=%q", code, stderr)
		}
		if _, _, code := execute(t, "project", "remove", "gone"); code != 0 {
			t.Fatal("remove setup failed")
		}
		dangling := filepath.Dir(derivedIndexPath(t, danglingRoot))
		claimed := filepath.Dir(derivedIndexPath(t, claimedRoot))

		stdout, _, code := execute(t, "project", "healthcheck", "--purge")
		if code != 0 || !strings.Contains(stdout, "Purged: "+dangling) {
			t.Fatalf("purge: code=%d stdout=%q", code, stdout)
		}
		if _, err := os.Stat(dangling); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("dangling directory survived purge: %v", err)
		}
		if _, err := os.Stat(filepath.Join(claimed, "index.json")); err != nil {
			t.Errorf("claimed index was touched: %v", err)
		}
	})

	t.Run("preserves unpurgeable dangling content", func(t *testing.T) {
		isolateState(t)
		apiRoot := t.TempDir()
		writeTestCard(t, apiRoot, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
		registerProject(t, "api", apiRoot)
		if _, stderr, code := execute(t, "index", "--project", "api"); code != 0 || stderr != "" {
			t.Fatalf("index setup: code=%d stderr=%q", code, stderr)
		}
		stateDir, err := indexpkg.StateDir()
		if err != nil {
			t.Fatal(err)
		}
		dirty := filepath.Join(stateDir, "unclaimed")
		if err := os.MkdirAll(dirty, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dirty, "index.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dirty, "extra.txt"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}

		stdout, stderr, code := execute(t, "project", "healthcheck", "--purge", "--format", "json")
		if code != 1 {
			t.Fatalf("purge: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var output healthcheckOutput
		if err := json.Unmarshal([]byte(stdout), &output); err != nil {
			t.Fatal(err)
		}
		if len(output.Purged) != 0 {
			t.Errorf("purged = %#v, want none", output.Purged)
		}
		if len(output.Findings) != 1 || output.Findings[0].Kind != "index_dangling" ||
			!strings.Contains(output.Findings[0].Detail, "not purged") {
			t.Errorf("findings = %#v, want a retained not-purged dangling finding", output.Findings)
		}
		if _, err := os.Stat(filepath.Join(dirty, "extra.txt")); err != nil {
			t.Errorf("dirty dangling directory was touched: %v", err)
		}
	})
}

func TestRunProjectRequiredUnknownAndRegistryErrors(t *testing.T) {
	t.Run("missing project flag", func(t *testing.T) {
		isolateState(t)
		stdout, stderr, code := execute(t, "search", "query")
		if code != 2 || stdout != "" || !strings.Contains(stderr, "--project is required") || !strings.Contains(stderr, "usage:") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		stdout, stderr, code = execute(t, "search", "--format=json", "query")
		assertJSONError(t, stdout, stderr, code, 2, "project_required")
	})

	t.Run("unknown project", func(t *testing.T) {
		isolateState(t)
		registerProject(t, "api", t.TempDir())
		stdout, stderr, code := execute(t, "search", "--project", "nope", "--format=json", "query")
		if code != 2 || stdout != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var output errorOutput
		if err := json.Unmarshal([]byte(stderr), &output); err != nil {
			t.Fatal(err)
		}
		if output.Error.Code != "project_unknown" || !strings.Contains(output.Error.Message, "configured projects: api") {
			t.Errorf("error = %#v, want project_unknown listing registry IDs", output)
		}
		stdout, stderr, code = execute(t, "index", "--project", "nope", "--format=json")
		assertJSONError(t, stdout, stderr, code, 2, "project_unknown")
		stdout, stderr, code = execute(t, "get", "--project", "nope", "--format=json", "card")
		assertJSONError(t, stdout, stderr, code, 2, "project_unknown")
	})

	t.Run("malformed project id", func(t *testing.T) {
		isolateState(t)
		registerProject(t, "api", t.TempDir())
		for _, id := range []string{"Bad_ID", "UPPER", "has space", ""} {
			stdout, stderr, code := execute(t, "search", "--project", id, "--format=json", "query")
			assertJSONError(t, stdout, stderr, code, 2, "project_id_invalid")
		}
	})

	t.Run("registry missing", func(t *testing.T) {
		isolateState(t)
		stdout, stderr, code := execute(t, "search", "--project", "api", "--format=json", "query")
		assertJSONError(t, stdout, stderr, code, 1, "project_registry_missing")
	})

	t.Run("registry invalid", func(t *testing.T) {
		isolateState(t)
		stateDir, err := indexpkg.StateDir()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "projects.json"), []byte("nonsense"), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := execute(t, "search", "--project", "api", "--format=json", "query")
		assertJSONError(t, stdout, stderr, code, 1, "project_registry_invalid")
	})

	t.Run("registry symlink refused", func(t *testing.T) {
		isolateState(t)
		stateDir, err := indexpkg.StateDir()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "registry.json")
		if err := os.WriteFile(target, []byte(`{"version":1,"projects":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(stateDir, "projects.json")); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := execute(t, "project", "list", "--format=json")
		assertJSONError(t, stdout, stderr, code, 1, "project_registry_invalid")
	})
}

func TestRunRootMissingBlocksProjectCommands(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	registerProject(t, "api", root)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := execute(t, "search", "--project", "api", "--format=json", "query")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var output errorOutput
	if err := json.Unmarshal([]byte(stderr), &output); err != nil {
		t.Fatal(err)
	}
	if output.Error.Code != "project_root_missing" || !strings.Contains(output.Error.Message, "wfm-rag project init api") {
		t.Errorf("error = %#v, want project_root_missing naming the repair", output)
	}
	stdout, stderr, code = execute(t, "index", "--project", "api", "--format=json")
	assertJSONError(t, stdout, stderr, code, 1, "project_root_missing")
	stdout, stderr, code = execute(t, "get", "--project", "api", "--format=json", "card")
	assertJSONError(t, stdout, stderr, code, 1, "project_root_missing")
}

func TestRunRetrievalCollapsesIndexLoadFailures(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	otherRoot := t.TempDir()
	writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\nNeedle."))
	registerProject(t, "api", root)
	built, err := indexpkg.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*indexpkg.Index)
		absent bool
	}{
		{name: "missing file", absent: true},
		{name: "unsupported version", mutate: func(value *indexpkg.Index) { value.Version = 2 }},
		{name: "root mismatch", mutate: func(value *indexpkg.Index) { value.Root = otherRoot }},
		{name: "invalid structure", mutate: func(value *indexpkg.Index) { value.Documents[0].Title = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "index.json")
			if !test.absent {
				value := cloneCLIIndex(t, built.Index)
				test.mutate(value)
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			stdout, stderr, code := execute(t, "search", "--project", "api", "--index", path, "--format=json", "needle")
			if code != 1 || stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var output errorOutput
			if err := json.Unmarshal([]byte(stderr), &output); err != nil {
				t.Fatal(err)
			}
			if output.Error.Code != "index_missing" {
				t.Errorf("error code = %q, want index_missing for every retrieval load failure", output.Error.Code)
			}
			if !strings.Contains(output.Error.Message, "wfm-rag index --project api") {
				t.Errorf("error message = %q, want the repair command", output.Error.Message)
			}
		})
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
		{name: "project required", args: []string{"index"}, want: "--project is required"},
		{name: "extra index argument", args: []string{"index", "--project", "api", "extra"}, want: "expected 0 positional argument"},
		{name: "missing query", args: []string{"search", "--project", "api"}, want: "expected 1 positional argument"},
		{name: "extra get id", args: []string{"get", "--project", "api", "a", "b"}, want: "expected 1 positional argument"},
		{name: "mcp project required", args: []string{"mcp"}, want: "--project is required"},
		{name: "mcp extra argument", args: []string{"mcp", "--project", "api", "extra"}, want: "expected 0 positional argument"},
		{name: "mcp combined selection", args: []string{"mcp", "--project", "api", "--all-projects"}, want: "cannot be combined"},
		{name: "root flag retired", args: []string{"index", "--root", "/tmp"}, want: "flag provided but not defined"},
		{name: "unknown flag", args: []string{"index", "--unknown"}, want: "flag provided but not defined"},
		{name: "bad format", args: []string{"index", "--project", "api", "--format", "xml"}, want: "unsupported format"},
		{name: "zero limit", args: []string{"search", "--project", "api", "--limit", "0", "query"}, want: "limit must be between"},
		{name: "bad limit syntax", args: []string{"search", "--project", "api", "--limit", "2x", "query"}, want: "invalid integer"},
		{name: "project repeated", args: []string{"index", "--project", "api", "--project", "web"}, want: "--project may be given once"},
		{name: "index repeated", args: []string{"search", "--project", "api", "--index", "/a.json", "--index", "/b.json", "query"}, want: "--index may be given once"},
		{name: "project subcommand required", args: []string{"project"}, want: "a project subcommand is required"},
		{name: "unknown project subcommand", args: []string{"project", "bogus"}, want: `unknown project subcommand "bogus"`},
		{name: "project init id required", args: []string{"project", "init", "--dir", "/tmp"}, want: "expected 1 positional argument"},
		{name: "project remove extra", args: []string{"project", "remove", "api", "extra"}, want: "expected 1 positional argument"},
		{name: "project list extra", args: []string{"project", "list", "extra"}, want: "expected 0 positional argument"},
		{name: "healthcheck extra", args: []string{"project", "healthcheck", "extra"}, want: "expected 0 positional argument"},
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

func TestRunMCPFlagAndRegistryErrors(t *testing.T) {
	t.Run("too many project flags", func(t *testing.T) {
		isolateState(t)
		args := []string{"mcp", "--format", "json"}
		for range maxConfiguredProjects + 1 {
			args = append(args, "--project", "api")
		}
		stdout, stderr, code := execute(t, args...)
		assertJSONError(t, stdout, stderr, code, 2, "project_limit_exceeded")
	})
	t.Run("unknown project at launch", func(t *testing.T) {
		isolateState(t)
		registerProject(t, "api", t.TempDir())
		stdout, stderr, code := execute(t, "mcp", "--project", "cloudfire", "--format", "json")
		if code != 2 || stdout != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var output errorOutput
		if err := json.Unmarshal([]byte(stderr), &output); err != nil {
			t.Fatal(err)
		}
		if output.Error.Code != "project_unknown" || !strings.Contains(output.Error.Message, "configured projects: api") {
			t.Errorf("error = %#v, want project_unknown listing registry IDs", output)
		}
	})

	t.Run("registry missing", func(t *testing.T) {
		isolateState(t)
		stdout, stderr, code := execute(t, "mcp", "--project", "api", "--format", "json")
		assertJSONError(t, stdout, stderr, code, 1, "project_registry_missing")
		stdout, stderr, code = execute(t, "mcp", "--all-projects", "--format", "json")
		assertJSONError(t, stdout, stderr, code, 1, "project_registry_missing")
	})
}

func TestRunClassifiesCorpusAndGetErrors(t *testing.T) {
	t.Run("duplicate card", func(t *testing.T) {
		isolateState(t)
		root := t.TempDir()
		writeTestCard(t, root, "patterns/first.md", testCard("same", "pattern", "active", "# First"))
		writeTestCard(t, root, "dependencies/second.md", testCard("same", "dependency", "active", "# Second"))
		registerProject(t, "api", root)
		stdout, stderr, code := execute(t, "index", "--project", "api", "--index", filepath.Join(t.TempDir(), "index.json"), "--format=json")
		assertJSONError(t, stdout, stderr, code, 1, "duplicate_card_id")
	})

	t.Run("malformed corpus", func(t *testing.T) {
		isolateState(t)
		root := t.TempDir()
		writeTestCard(t, root, "patterns/card.md", "not a card")
		registerProject(t, "api", root)
		stdout, stderr, code := execute(t, "index", "--project", "api", "--index", filepath.Join(t.TempDir(), "index.json"), "--format=json")
		assertJSONError(t, stdout, stderr, code, 1, "malformed_card_corpus")
	})

	t.Run("card not found", func(t *testing.T) {
		isolateState(t)
		root := t.TempDir()
		path := filepath.Join(t.TempDir(), "index.json")
		writeTestCard(t, root, "patterns/card.md", testCard("card", "pattern", "active", "# Card"))
		registerProject(t, "api", root)
		if _, _, code := execute(t, "index", "--project", "api", "--index", path); code != 0 {
			t.Fatal("index setup failed")
		}
		stdout, stderr, code := execute(t, "get", "--project", "api", "--index", path, "--format=json", "missing")
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
	isolateState(t)
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

func TestCLIProcessReportsProjectRequired(t *testing.T) {
	isolateState(t)
	command := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--", "search", "--format=json", "query")
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
	if output.Error.Code != "project_required" {
		t.Errorf("error code = %q", output.Error.Code)
	}
}

func TestCLIProcessKeepsOperationalErrorsOnStderr(t *testing.T) {
	isolateState(t)
	command := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--", "search", "--project", "api", "--format=json", "query")
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
	if output.Error.Code != "project_registry_missing" {
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

// isolateState points HOME at a fresh directory and clears XDG_DATA_HOME so the
// registry and derived indexes land inside the test's own state directory.
func isolateState(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	return home
}

func registerProject(t *testing.T, id, dir string) {
	t.Helper()
	stdout, stderr, code := execute(t, "project", "init", id, "--dir", dir)
	if code != 0 || stderr != "" {
		t.Fatalf("project init %s setup: code=%d stdout=%q stderr=%q", id, code, stdout, stderr)
	}
}

func derivedIndexPath(t *testing.T, root string) string {
	t.Helper()
	stateDir, err := indexpkg.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(root))
	return filepath.Join(stateDir, hex.EncodeToString(digest[:]), "index.json")
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
