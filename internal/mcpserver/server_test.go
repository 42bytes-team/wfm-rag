package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	indexpkg "wfm-rag/internal/index"
	"wfm-rag/internal/search"
)

func TestToolsExposeStructuredSearchAndGet(t *testing.T) {
	root, indexPath, content := buildTestIndex(t, "Use Key.Strings for comma-separated config.")
	server, warnings, err := New(context.Background(), []Source{{ID: "primary", Root: root, IndexPath: indexPath}})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for a valid index", warnings)
	}
	session := connectTestClient(t, server)

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 2 || listed.Tools[0].Name != getToolName || listed.Tools[1].Name != searchToolName {
		t.Fatalf("tools = %#v", listed.Tools)
	}
	for _, tool := range listed.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("tool %q annotations = %#v", tool.Name, tool.Annotations)
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(schema), `"root"`) {
			t.Errorf("tool %q accepts a root selector: %s", tool.Name, schema)
		}
	}

	searched := callTool(t, session, searchToolName, map[string]any{
		"project":  "primary",
		"query":    "config parser comma separated",
		"packages": []string{"gopkg.in/ini.v1"},
	})
	var response search.Response
	decodeStructured(t, searched, &response)
	if response.Project != "primary" || response.Root != root || len(response.Results) != 1 ||
		response.Results[0].ID != "ini-list-parsing" {
		t.Fatalf("search response = %#v", response)
	}
	assertTextFallback(t, searched, response)

	got := callTool(t, session, getToolName, map[string]any{"project": "primary", "id": "ini-list-parsing"})
	var getResponse search.GetResponse
	decodeStructured(t, got, &getResponse)
	if getResponse.Project != "primary" || getResponse.Card.Content != content ||
		getResponse.Card.Source != ".agents/knowledge/dependencies/ini.md" || len(getResponse.Card.Paths) != 1 {
		t.Fatalf("get response = %#v", getResponse)
	}
	assertTextFallback(t, got, getResponse)
}

func TestToolSchemasRequireProjectEnumAndClosedWorld(t *testing.T) {
	alpha, _ := buildNamedIndex(t, "alpha", "alpha-card", "Alpha Card", "alpha", "Use alphaone guidance.")
	zeta, _ := buildNamedIndex(t, "zeta", "zeta-card", "Zeta Card", "zeta", "Use zetaone guidance.")
	server, _, err := New(context.Background(), []Source{zeta, alpha})
	if err != nil {
		t.Fatal(err)
	}
	session := connectTestClient(t, server)

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 2 {
		t.Fatalf("tools = %#v, want exactly two tools", listed.Tools)
	}

	wantEnum := []any{"alpha", "zeta"}
	wantProperties := map[string][]string{
		getToolName:    {"id", "project"},
		searchToolName: {"limit", "packages", "paths", "project", "query", "types"},
	}
	for _, tool := range listed.Tools {
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Required             []string                   `json:"required"`
			Properties           map[string]json.RawMessage `json:"properties"`
			AdditionalProperties json.RawMessage            `json:"additionalProperties"`
		}
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatalf("tool %q input schema: %v\n%s", tool.Name, err, encoded)
		}
		if !containsString(schema.Required, "project") {
			t.Errorf("tool %q required = %v, want project required", tool.Name, schema.Required)
		}
		project, ok := schema.Properties["project"]
		if !ok {
			t.Fatalf("tool %q properties = %v, want a project property", tool.Name, sortedKeys(schema.Properties))
		}
		var projectSchema struct {
			Enum []any `json:"enum"`
		}
		if err := json.Unmarshal(project, &projectSchema); err != nil {
			t.Fatalf("tool %q project schema: %v\n%s", tool.Name, err, project)
		}
		if !reflect.DeepEqual(projectSchema.Enum, wantEnum) {
			t.Errorf("tool %q project enum = %v, want the launch IDs sorted", tool.Name, projectSchema.Enum)
		}
		if got := sortedKeys(schema.Properties); !reflect.DeepEqual(got, wantProperties[tool.Name]) {
			t.Errorf("tool %q properties = %v, want exactly %v", tool.Name, got, wantProperties[tool.Name])
		}
		if string(schema.AdditionalProperties) != "false" {
			t.Errorf("tool %q additionalProperties = %s, want closed-world false", tool.Name, schema.AdditionalProperties)
		}
	}
}

func TestToolValidationAndNotFoundErrors(t *testing.T) {
	root, indexPath, _ := buildTestIndex(t, "Config parser guidance.")
	server, _, err := New(context.Background(), []Source{{ID: "primary", Root: root, IndexPath: indexPath}})
	if err != nil {
		t.Fatal(err)
	}
	session := connectTestClient(t, server)

	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
		want      string
	}{
		{name: "unknown root field", tool: searchToolName, arguments: map[string]any{"project": "primary", "query": "config", "root": t.TempDir()}, want: "validating"},
		{name: "index path field", tool: searchToolName, arguments: map[string]any{"project": "primary", "query": "config", "index_path": "elsewhere.json"}, want: "validating"},
		{name: "missing project", tool: searchToolName, arguments: map[string]any{"query": "config"}, want: "validating"},
		{name: "empty query", tool: searchToolName, arguments: map[string]any{"project": "primary", "query": " "}, want: "invalid search arguments"},
		{name: "invalid limit", tool: searchToolName, arguments: map[string]any{"project": "primary", "query": "config", "limit": search.MaxLimit + 1}, want: "limit must be between"},
		{name: "missing card", tool: getToolName, arguments: map[string]any{"project": "primary", "id": "missing"}, want: "card not found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: test.tool, Arguments: test.arguments})
			if err != nil {
				t.Fatalf("protocol error: %v", err)
			}
			if !result.IsError || !strings.Contains(toolText(t, result), test.want) {
				t.Errorf("result = %#v, want tool error containing %q", result, test.want)
			}
		})
	}
}

func TestUnknownProjectIsRejectedWithoutIndexAccess(t *testing.T) {
	alpha, _ := buildNamedIndex(t, "alpha", "alpha-card", "Alpha Card", "alpha", "Use alphaone guidance.")
	beta, _ := buildNamedIndex(t, "beta", "beta-card", "Beta Card", "beta", "Use betaone guidance.")
	server, _, err := New(context.Background(), []Source{alpha, beta})
	if err != nil {
		t.Fatal(err)
	}
	session := connectTestClient(t, server)

	ghostIndex := filepath.Join(t.TempDir(), "ghost-index.json")
	if _, err := os.Stat(ghostIndex); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ghost index = %v, want no index file for the unknown selection", err)
	}

	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
	}{
		{name: "search", tool: searchToolName, arguments: map[string]any{"project": "ghost", "query": "guidance"}},
		{name: "get", tool: getToolName, arguments: map[string]any{"project": "ghost", "id": "alpha-card"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: test.tool, Arguments: test.arguments})
			if err != nil {
				t.Fatalf("protocol error: %v", err)
			}
			text := toolText(t, result)
			if !result.IsError || !strings.Contains(text, "validating") || !strings.Contains(text, "enum") || !strings.Contains(text, "alpha") || !strings.Contains(text, "beta") {
				t.Fatalf("result = %#v, want a tool error naming the configured projects without touching any index", result)
			}
			if strings.Contains(text, "index_missing") || strings.Contains(text, ErrReload.Error()) {
				t.Errorf("tool error = %q, want no index access for an unknown project", text)
			}
		})
	}
}

func TestProjectSelectionIsolatesCorpora(t *testing.T) {
	alphaRoot := t.TempDir()
	betaRoot := t.TempDir()
	alphaIndex := filepath.Join(t.TempDir(), "alpha-index.json")
	betaIndex := filepath.Join(t.TempDir(), "beta-index.json")

	alphaShared := writeCardFixture(t, alphaRoot, "shared-card", "Alpha Shared", "shared", "Use sharedalpha guidance.")
	writeCardFixture(t, alphaRoot, "alpha-card", "Alpha Card", "alpha", "Use alphaone guidance.")
	if _, err := indexpkg.Rebuild(alphaRoot, alphaIndex); err != nil {
		t.Fatal(err)
	}
	betaShared := writeCardFixture(t, betaRoot, "shared-card", "Beta Shared", "shared", "Use sharedbeta guidance.")
	writeCardFixture(t, betaRoot, "beta-card", "Beta Card", "beta", "Use betaone guidance.")
	if _, err := indexpkg.Rebuild(betaRoot, betaIndex); err != nil {
		t.Fatal(err)
	}

	server, warnings, err := New(context.Background(), []Source{
		{ID: "alpha", Root: alphaRoot, IndexPath: alphaIndex},
		{ID: "beta", Root: betaRoot, IndexPath: betaIndex},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	session := connectTestClient(t, server)

	type selection struct {
		project  string
		root     string
		wantIDs  []string
		forbidID string
	}
	for _, test := range []selection{
		{project: "alpha", root: alphaRoot, wantIDs: []string{"alpha-card", "shared-card"}, forbidID: "beta-card"},
		{project: "beta", root: betaRoot, wantIDs: []string{"beta-card", "shared-card"}, forbidID: "alpha-card"},
	} {
		searched := callTool(t, session, searchToolName, map[string]any{"project": test.project, "query": "guidance parser"})
		var response search.Response
		decodeStructured(t, searched, &response)
		if response.Project != test.project || response.Root != test.root {
			t.Errorf("search envelope = project %q root %q, want %q %q", response.Project, response.Root, test.project, test.root)
		}
		if got := resultIDs(response.Results); !reflect.DeepEqual(got, test.wantIDs) {
			t.Errorf("project %q search IDs = %v, want %v", test.project, got, test.wantIDs)
		}
	}

	for _, test := range []struct {
		project   string
		wantTitle string
		wantBody  string
	}{
		{project: "alpha", wantTitle: "Alpha Shared", wantBody: alphaShared},
		{project: "beta", wantTitle: "Beta Shared", wantBody: betaShared},
	} {
		got := callTool(t, session, getToolName, map[string]any{"project": test.project, "id": "shared-card"})
		var getResponse search.GetResponse
		decodeStructured(t, got, &getResponse)
		if getResponse.Project != test.project || getResponse.Card.Title != test.wantTitle || getResponse.Card.Content != test.wantBody {
			t.Errorf("project %q get shared-card = %#v, want the %q card only", test.project, getResponse.Card, test.project)
		}
	}
}

func TestDegradedStartupWarnsAndRecoversWithoutRestart(t *testing.T) {
	alpha, _ := buildNamedIndex(t, "alpha", "alpha-card", "Alpha Card", "alpha", "Use alphaone guidance.")
	betaRoot := t.TempDir()
	betaIndex := filepath.Join(t.TempDir(), "beta-index.json")
	writeCardFixture(t, betaRoot, "beta-card", "Beta Card", "beta", "Use betaone guidance.")

	server, warnings, err := New(context.Background(), []Source{
		alpha,
		{ID: "beta", Root: betaRoot, IndexPath: betaIndex},
	})
	if err != nil {
		t.Fatalf("degraded New error = %v, want a usable server", err)
	}
	if len(warnings) != 1 || warnings[0].Project != "beta" || warnings[0].Path != betaIndex ||
		!errors.Is(warnings[0].Err, indexpkg.ErrMissing) {
		t.Fatalf("warnings = %#v, want one beta warning for the missing index", warnings)
	}
	session := connectTestClient(t, server)

	searched := callTool(t, session, searchToolName, map[string]any{"project": "alpha", "query": "guidance parser"})
	var response search.Response
	decodeStructured(t, searched, &response)
	if response.Project != "alpha" || len(response.Results) == 0 {
		t.Fatalf("healthy project response = %#v", response)
	}

	failed := callToolError(t, session, searchToolName, map[string]any{"project": "beta", "query": "guidance parser"})
	text := toolText(t, failed)
	for _, want := range []string{"index_missing", `"beta"`, "wfm-rag index --project beta"} {
		if !strings.Contains(text, want) {
			t.Errorf("degraded tool error = %q, want it to contain %q", text, want)
		}
	}

	if _, err := indexpkg.Rebuild(betaRoot, betaIndex); err != nil {
		t.Fatal(err)
	}
	recovered := callTool(t, session, searchToolName, map[string]any{"project": "beta", "query": "guidance parser"})
	var recoveredResponse search.Response
	decodeStructured(t, recovered, &recoveredResponse)
	if recoveredResponse.Project != "beta" || recoveredResponse.Root != betaRoot ||
		!reflect.DeepEqual(resultIDs(recoveredResponse.Results), []string{"beta-card"}) {
		t.Fatalf("recovered response = %#v, want beta served without re-creating the server", recoveredResponse)
	}
}

func TestBrokenProjectIndexIsScopedToItsProject(t *testing.T) {
	alpha, _ := buildNamedIndex(t, "alpha", "alpha-card", "Alpha Card", "alpha", "Original alphaone guidance.")
	beta, _ := buildNamedIndex(t, "beta", "beta-card", "Beta Card", "beta", "Original betaone guidance.")
	server, warnings, err := New(context.Background(), []Source{alpha, beta})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	session := connectTestClient(t, server)

	temporary := filepath.Join(filepath.Dir(beta.IndexPath), "malformed.tmp")
	if err := os.WriteFile(temporary, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, beta.IndexPath); err != nil {
		t.Fatal(err)
	}

	searched := callTool(t, session, searchToolName, map[string]any{"project": "alpha", "query": "original guidance"})
	var response search.Response
	decodeStructured(t, searched, &response)
	if response.Project != "alpha" || !reflect.DeepEqual(resultIDs(response.Results), []string{"alpha-card"}) {
		t.Fatalf("healthy selection with a broken sibling = %#v, want the alpha index served", response)
	}

	failed := callToolError(t, session, searchToolName, map[string]any{"project": "beta", "query": "original guidance"})
	text := toolText(t, failed)
	for _, want := range []string{ErrReload.Error(), "index_missing", "wfm-rag index --project beta"} {
		if !strings.Contains(text, want) {
			t.Errorf("broken selection tool error = %q, want it to contain %q", text, want)
		}
	}

	invalid := callToolError(t, session, searchToolName, map[string]any{"project": "beta", "query": " "})
	if !strings.Contains(toolText(t, invalid), "invalid search arguments") || strings.Contains(toolText(t, invalid), ErrReload.Error()) {
		t.Errorf("invalid input with a broken index = %q, want validation before any reload", toolText(t, invalid))
	}

	writeCardFixture(t, beta.Root, "beta-card", "Beta Card", "beta", "Use replacementword guidance.")
	if _, err := indexpkg.Rebuild(beta.Root, beta.IndexPath); err != nil {
		t.Fatal(err)
	}
	recovered := callTool(t, session, searchToolName, map[string]any{"project": "beta", "query": "replacementword"})
	var recoveredResponse search.Response
	decodeStructured(t, recovered, &recoveredResponse)
	if !reflect.DeepEqual(resultIDs(recoveredResponse.Results), []string{"beta-card"}) ||
		!strings.Contains(recoveredResponse.Results[0].Excerpt, "replacementword") {
		t.Fatalf("recovered response = %#v, want beta recovered on the next call", recoveredResponse)
	}
}

func TestReloadFailureDoesNotServeStaleIndexAndRecovers(t *testing.T) {
	root, indexPath, _ := buildTestIndex(t, "Original config guidance.")
	server, _, err := New(context.Background(), []Source{{ID: "primary", Root: root, IndexPath: indexPath}})
	if err != nil {
		t.Fatal(err)
	}
	session := connectTestClient(t, server)

	temporary := filepath.Join(filepath.Dir(indexPath), "malformed.tmp")
	if err := os.WriteFile(temporary, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, indexPath); err != nil {
		t.Fatal(err)
	}
	failed := callToolError(t, session, searchToolName, map[string]any{"project": "primary", "query": "original"})
	if !strings.Contains(toolText(t, failed), ErrReload.Error()) {
		t.Fatalf("reload result = %#v", failed)
	}
	invalid := callToolError(t, session, searchToolName, map[string]any{"project": "primary", "query": " "})
	if !strings.Contains(toolText(t, invalid), "invalid search arguments") || strings.Contains(toolText(t, invalid), ErrReload.Error()) {
		t.Fatalf("invalid input result = %#v", invalid)
	}

	writeTestCard(t, root, "Use replacementword guidance.")
	if _, err := indexpkg.Rebuild(root, indexPath); err != nil {
		t.Fatal(err)
	}
	reloaded := callTool(t, session, searchToolName, map[string]any{"project": "primary", "query": "replacementword"})
	var response search.Response
	decodeStructured(t, reloaded, &response)
	if len(response.Results) != 1 || response.Results[0].ID != "ini-list-parsing" || !strings.Contains(response.Results[0].Excerpt, "replacementword") {
		t.Fatalf("reloaded response = %#v", response)
	}
}

func TestReloadDetectsAtomicReplacementWithSameSizeAndModTime(t *testing.T) {
	root, indexPath, _ := buildTestIndex(t, "Use alphaone guidance.")
	server, _, err := New(context.Background(), []Source{{ID: "primary", Root: root, IndexPath: indexPath}})
	if err != nil {
		t.Fatal(err)
	}
	session := connectTestClient(t, server)
	before, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	original, err := indexpkg.Load(indexPath, root)
	if err != nil {
		t.Fatal(err)
	}

	writeTestCard(t, root, "Use betatwoo guidance.")
	replacement, err := indexpkg.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	replacement.Index.GeneratedAt = original.GeneratedAt
	if err := indexpkg.Write(indexPath, replacement.Index); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(indexPath, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || os.SameFile(before, after) {
		t.Fatalf("replacement fingerprint differs beyond identity: before=%#v after=%#v", before, after)
	}

	reloaded := callTool(t, session, searchToolName, map[string]any{"project": "primary", "query": "betatwoo"})
	var response search.Response
	decodeStructured(t, reloaded, &response)
	if len(response.Results) != 1 || response.Results[0].ID != "ini-list-parsing" || !strings.Contains(response.Results[0].Excerpt, "betatwoo") {
		t.Fatalf("reloaded response = %#v", response)
	}
}

func TestServerRunStopsWhenContextIsCancelled(t *testing.T) {
	root, indexPath, _ := buildTestIndex(t, "Config guidance.")
	server, _, err := New(context.Background(), []Source{{ID: "primary", Root: root, IndexPath: indexPath}})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "wfm-rag-test", Version: "0.1.0"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer session.Close()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

func TestNewHonorsPreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := New(ctx, []Source{{ID: "primary", Root: t.TempDir(), IndexPath: filepath.Join(t.TempDir(), "missing.json")}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("New error = %v, want context cancellation", err)
	}
}

func TestNewRejectsInvalidSourceSets(t *testing.T) {
	if _, _, err := New(context.Background(), nil); err == nil {
		t.Error("New(nil) error = nil, want an error for an empty source set")
	}
	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "index.json")
	duplicate := Source{ID: "primary", Root: root, IndexPath: indexPath}
	if _, _, err := New(context.Background(), []Source{duplicate, duplicate}); err == nil {
		t.Error("New(duplicate) error = nil, want an error for a duplicate project ID")
	}
	if _, _, err := New(context.Background(), []Source{{ID: " ", Root: root, IndexPath: indexPath}}); err == nil {
		t.Error("New(blank ID) error = nil, want an error for a blank project ID")
	}
}

func TestUnknownProjectDefenseAtHandlerLevel(t *testing.T) {
	alpha, _ := buildNamedIndex(t, "alpha", "alpha-card", "Alpha Card", "alpha", "Use alphaone guidance.")
	beta, _ := buildNamedIndex(t, "beta", "beta-card", "Beta Card", "beta", "Use betaone guidance.")

	state := &serverState{projects: map[string]*projectState{
		"beta":  {id: "beta", root: beta.Root, path: beta.IndexPath},
		"alpha": {id: "alpha", root: alpha.Root, path: alpha.IndexPath},
	}}

	_, _, searchErr := state.search(context.Background(), nil, SearchInput{Project: "ghost", Query: "guidance"})
	if searchErr == nil || !strings.Contains(searchErr.Error(), "project_unknown") ||
		!strings.Contains(searchErr.Error(), "alpha, beta") || strings.Contains(searchErr.Error(), "index_missing") ||
		strings.Contains(searchErr.Error(), ErrReload.Error()) {
		t.Errorf("handler search error = %v, want project_unknown naming the sorted configured IDs with no index load", searchErr)
	}
	_, _, getErr := state.get(context.Background(), nil, GetInput{Project: "ghost", ID: "alpha-card"})
	if getErr == nil || !strings.Contains(getErr.Error(), "project_unknown") ||
		!strings.Contains(getErr.Error(), "alpha, beta") || strings.Contains(getErr.Error(), "index_missing") ||
		strings.Contains(getErr.Error(), ErrReload.Error()) {
		t.Errorf("handler get error = %v, want project_unknown naming the sorted configured IDs with no index load", getErr)
	}
}

func buildTestIndex(t *testing.T, guidance string) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "index.json")
	content := writeTestCard(t, root, guidance)
	if _, err := indexpkg.Rebuild(root, indexPath); err != nil {
		t.Fatal(err)
	}
	return root, indexPath, content
}

func writeTestCard(t *testing.T, root, guidance string) string {
	t.Helper()
	return writeCardFixture(t, root, "ini-list-parsing", "INI List Parsing", "ini", guidance)
}

func writeCardFixture(t *testing.T, root, id, title, stem, guidance string) string {
	t.Helper()
	content := "---\n" +
		"id: " + id + "\n" +
		"type: dependency\n" +
		"status: active\n" +
		"scope: [config]\n" +
		"triggers: [parser, comma-separated]\n" +
		"packages: [gopkg.in/ini.v1]\n" +
		"paths: [src/config/ini.go]\n" +
		"source_confidence: high\n" +
		"last_verified: 2026-08-14\n" +
		"---\n\n# " + title + "\n\n## Guidance\n\n" + guidance + "\n"
	path := filepath.Join(root, ".agents", "knowledge", "dependencies", stem+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}

// buildNamedIndex creates an isolated corpus and index for one configured project.
func buildNamedIndex(t *testing.T, project, id, title, stem, guidance string) (Source, string) {
	t.Helper()
	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "index.json")
	content := writeCardFixture(t, root, id, title, stem, guidance)
	if _, err := indexpkg.Rebuild(root, indexPath); err != nil {
		t.Fatal(err)
	}
	return Source{ID: project, Root: root, IndexPath: indexPath}, content
}

func connectTestClient(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "wfm-rag-test", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		serverSession.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clientSession.Close()
		serverSession.Close()
	})
	return clientSession
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("call %s returned tool error: %s", name, toolText(t, result))
	}
	return result
}

func callToolError(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if !result.IsError {
		t.Fatalf("call %s succeeded, want a tool error: %#v", name, result)
	}
	return result
}

func decodeStructured(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("decode structured content: %v\n%s", err, encoded)
	}
}

func assertTextFallback(t *testing.T, result *mcp.CallToolResult, want any) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal([]byte(toolText(t, result)), &gotValue); err != nil {
		t.Fatalf("text fallback is not JSON: %v", err)
	}
	if err := json.Unmarshal(wantJSON, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("text fallback = %#v, want %#v", gotValue, wantValue)
	}
}

func toolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v", result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T", result.Content[0])
	}
	return text.Text
}

func resultIDs(results []search.Result) []string {
	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.ID
	}
	return ids
}

func sortedKeys[V any](properties map[string]V) []string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
