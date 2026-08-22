package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	indexpkg "wfm-rag/internal/index"
	"wfm-rag/internal/search"
)

func TestToolsExposeStructuredSearchAndGet(t *testing.T) {
	root, indexPath, content := buildTestIndex(t, "Use Key.Strings for comma-separated config.")
	server, err := New(root, indexPath)
	if err != nil {
		t.Fatal(err)
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
		if strings.Contains(string(schema), `"root"`) || strings.Contains(string(schema), `"project"`) {
			t.Errorf("tool %q accepts a root selector: %s", tool.Name, schema)
		}
	}

	searched := callTool(t, session, searchToolName, map[string]any{
		"query":    "config parser comma separated",
		"packages": []string{"gopkg.in/ini.v1"},
	})
	var response search.Response
	decodeStructured(t, searched, &response)
	if response.Root != root || len(response.Results) != 1 || response.Results[0].ID != "ini-list-parsing" {
		t.Fatalf("search response = %#v", response)
	}
	assertTextFallback(t, searched, response)

	got := callTool(t, session, getToolName, map[string]any{"id": "ini-list-parsing"})
	var getResponse search.GetResponse
	decodeStructured(t, got, &getResponse)
	if getResponse.Card.Content != content || getResponse.Card.Source != ".agents/knowledge/dependencies/ini.md" || len(getResponse.Card.Paths) != 1 {
		t.Fatalf("get response = %#v", getResponse)
	}
	assertTextFallback(t, got, getResponse)
}

func TestToolValidationAndNotFoundErrors(t *testing.T) {
	root, indexPath, _ := buildTestIndex(t, "Config parser guidance.")
	server, err := New(root, indexPath)
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
		{name: "unknown root field", tool: searchToolName, arguments: map[string]any{"query": "config", "root": t.TempDir()}, want: "validating"},
		{name: "empty query", tool: searchToolName, arguments: map[string]any{"query": " "}, want: "invalid search arguments"},
		{name: "invalid limit", tool: searchToolName, arguments: map[string]any{"query": "config", "limit": search.MaxLimit + 1}, want: "limit must be between"},
		{name: "missing card", tool: getToolName, arguments: map[string]any{"id": "missing"}, want: "card not found"},
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

func TestReloadFailureDoesNotServeStaleIndexAndRecovers(t *testing.T) {
	root, indexPath, _ := buildTestIndex(t, "Original config guidance.")
	server, err := New(root, indexPath)
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
	failed, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      searchToolName,
		Arguments: map[string]any{"query": "original"},
	})
	if err != nil {
		t.Fatalf("protocol error: %v", err)
	}
	if !failed.IsError || !strings.Contains(toolText(t, failed), ErrReload.Error()) {
		t.Fatalf("reload result = %#v", failed)
	}
	invalid, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      searchToolName,
		Arguments: map[string]any{"query": " "},
	})
	if err != nil {
		t.Fatalf("protocol error: %v", err)
	}
	if !invalid.IsError || !strings.Contains(toolText(t, invalid), "invalid search arguments") || strings.Contains(toolText(t, invalid), ErrReload.Error()) {
		t.Fatalf("invalid input result = %#v", invalid)
	}

	writeTestCard(t, root, "Use replacementword guidance.")
	if _, err := indexpkg.Rebuild(root, indexPath); err != nil {
		t.Fatal(err)
	}
	reloaded := callTool(t, session, searchToolName, map[string]any{"query": "replacementword"})
	var response search.Response
	decodeStructured(t, reloaded, &response)
	if len(response.Results) != 1 || response.Results[0].ID != "ini-list-parsing" || !strings.Contains(response.Results[0].Excerpt, "replacementword") {
		t.Fatalf("reloaded response = %#v", response)
	}
}

func TestReloadDetectsAtomicReplacementWithSameSizeAndModTime(t *testing.T) {
	root, indexPath, _ := buildTestIndex(t, "Use alphaone guidance.")
	server, err := New(root, indexPath)
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

	reloaded := callTool(t, session, searchToolName, map[string]any{"query": "betatwoo"})
	var response search.Response
	decodeStructured(t, reloaded, &response)
	if len(response.Results) != 1 || response.Results[0].ID != "ini-list-parsing" || !strings.Contains(response.Results[0].Excerpt, "betatwoo") {
		t.Fatalf("reloaded response = %#v", response)
	}
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	root, indexPath, _ := buildTestIndex(t, "Config guidance.")
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, root, indexPath, serverTransport)
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

func TestRunHonorsPreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	serverTransport, _ := mcp.NewInMemoryTransports()
	err := Run(ctx, t.TempDir(), filepath.Join(t.TempDir(), "missing.json"), serverTransport)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context cancellation", err)
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
	content := "---\n" +
		"id: ini-list-parsing\n" +
		"type: dependency\n" +
		"status: active\n" +
		"scope: [config]\n" +
		"triggers: [parser, comma-separated]\n" +
		"packages: [gopkg.in/ini.v1]\n" +
		"paths: [src/config/ini.go]\n" +
		"source_confidence: high\n" +
		"last_verified: 2026-08-14\n" +
		"---\n\n# INI List Parsing\n\n## Guidance\n\n" + guidance + "\n"
	path := filepath.Join(root, ".agents", "knowledge", "dependencies", "ini.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return content
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
