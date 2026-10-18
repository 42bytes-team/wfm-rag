package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	indexpkg "wfm-rag/internal/index"
	"wfm-rag/internal/mcpserver"
	"wfm-rag/internal/search"
)

const (
	protocolVersion       = "2026-07-28"
	maxProtocolLineBytes  = 4 << 20
	cancelHelperEnv       = "GO_WANT_MCP_CANCEL_HELPER"
	cancelHelperRootEnv   = "MCP_CANCEL_ROOT"
	cancelHelperIndexEnv  = "MCP_CANCEL_INDEX"
	blockingTestToolName  = "blocking_test_tool"
	protocolResponseLimit = 5 * time.Second
)

func TestCorpusCLIAndMCPRetrievalAreEquivalent(t *testing.T) {
	isolateState(t)
	root := mutableIntegrationCorpusRoot(t)
	registerProject(t, "corpus", root)
	if stdout, stderr, code := execute(t, "index", "--project", "corpus"); code != 0 || stderr != "" {
		t.Fatalf("index setup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	derived := derivedIndexPath(t, root)

	server, warnings, err := mcpserver.New(context.Background(), []mcpserver.Source{
		{ID: "corpus", Root: root, IndexPath: derived},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("startup warnings = %#v, want none", warnings)
	}
	session := connectIntegrationClient(t, server)

	tests := []struct {
		query string
		want  string
	}{
		{query: "config parser comma separated", want: "ini-list-parsing"},
		{query: "pointer helper", want: "modern-go"},
	}
	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			stdout, stderr, code := execute(t, "search", "--project", "corpus", "--format=json", test.query)
			if code != 0 || stderr != "" {
				t.Fatalf("CLI search code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var cliResponse search.Response
			if err := json.Unmarshal([]byte(stdout), &cliResponse); err != nil {
				t.Fatal(err)
			}
			mcpResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "agent_knowledge_search",
				Arguments: map[string]any{"project": "corpus", "query": test.query},
			})
			if err != nil || mcpResult.IsError {
				t.Fatalf("MCP search result=%#v error=%v", mcpResult, err)
			}
			var mcpResponse search.Response
			decodeMCPStructured(t, mcpResult, &mcpResponse)
			if !reflect.DeepEqual(mcpResponse, cliResponse) {
				t.Fatalf("MCP response differs from CLI\nMCP: %#v\nCLI: %#v", mcpResponse, cliResponse)
			}
			if mcpResponse.Project != "corpus" {
				t.Errorf("response project = %q, want corpus", mcpResponse.Project)
			}
			if len(cliResponse.Results) == 0 || cliResponse.Results[0].ID != test.want {
				t.Fatalf("results = %#v, want %q first", cliResponse.Results, test.want)
			}
		})
	}

	stdout, stderr, code := execute(t, "get", "--project", "corpus", "--format=json", "ini-list-parsing")
	if code != 0 || stderr != "" {
		t.Fatalf("CLI get code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var cliGet search.GetResponse
	if err := json.Unmarshal([]byte(stdout), &cliGet); err != nil {
		t.Fatal(err)
	}
	mcpResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "agent_knowledge_get",
		Arguments: map[string]any{"project": "corpus", "id": "ini-list-parsing"},
	})
	if err != nil || mcpResult.IsError {
		t.Fatalf("MCP get result=%#v error=%v", mcpResult, err)
	}
	var mcpGet search.GetResponse
	decodeMCPStructured(t, mcpResult, &mcpGet)
	if !reflect.DeepEqual(mcpGet, cliGet) || !strings.Contains(mcpGet.Card.Content, `Key.Strings(",")`) {
		t.Fatalf("MCP get differs from CLI or lacks guidance\nMCP: %#v\nCLI: %#v", mcpGet, cliGet)
	}
}

func TestMCPStdioProtocolAndEOF(t *testing.T) {
	isolateState(t)
	root := mutableIntegrationCorpusRoot(t)
	registerProject(t, "corpus", root)
	if stdout, stderr, code := execute(t, "index", "--project", "corpus"); code != 0 || stderr != "" {
		t.Fatalf("index setup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	derived := derivedIndexPath(t, root)

	process := startCLIHelper(t, "mcp", "--project", "corpus")
	defer process.cleanup()

	sendRPC(t, process.encoder, 1, "server/discover", map[string]any{"_meta": protocolMeta()})
	discover := readRPC(t, process.stdout, 1)
	var discovered struct {
		SupportedVersions []string `json:"supportedVersions"`
		Capabilities      struct {
			Tools *struct {
				ListChanged bool `json:"listChanged"`
			} `json:"tools"`
		} `json:"capabilities"`
	}
	decodeRPCResult(t, discover, &discovered)
	if len(discovered.SupportedVersions) == 0 || discovered.SupportedVersions[0] != protocolVersion || discovered.Capabilities.Tools == nil || !discovered.Capabilities.Tools.ListChanged {
		t.Fatalf("discover result = %#v", discovered)
	}

	sendRPC(t, process.encoder, 2, "tools/list", map[string]any{"_meta": protocolMeta()})
	listed := readRPC(t, process.stdout, 2)
	tools := decodeToolSchemas(t, listed)
	if len(tools) != 2 || tools[0].Name != "agent_knowledge_get" || tools[1].Name != "agent_knowledge_search" {
		t.Fatalf("tools = %#v", tools)
	}
	for _, tool := range tools {
		assertProjectEnum(t, tool, []string{"corpus"})
	}

	sendRPC(t, process.encoder, 3, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "corpus", "query": "config parser comma separated"},
	})
	searchWire := decodeWireToolResult(t, readRPC(t, process.stdout, 3))
	var searched search.Response
	if err := json.Unmarshal(searchWire, &searched); err != nil {
		t.Fatal(err)
	}
	if searched.Project != "corpus" || len(searched.Results) == 0 || searched.Results[0].ID != "ini-list-parsing" {
		t.Fatalf("search response = %#v", searched)
	}

	sendRPC(t, process.encoder, 4, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_get",
		"arguments": map[string]any{"project": "corpus", "id": "ini-list-parsing"},
	})
	getWire := decodeWireToolResult(t, readRPC(t, process.stdout, 4))
	var got search.GetResponse
	if err := json.Unmarshal(getWire, &got); err != nil {
		t.Fatal(err)
	}
	if got.Project != "corpus" || got.Card.ID != "ini-list-parsing" || !strings.Contains(got.Card.Content, `Key.Strings(",")`) {
		t.Fatalf("get response = %#v", got)
	}

	cardPath := filepath.Join(root, ".agents", "knowledge", "dependencies", "ini-list-parsing.md")
	cardContent, err := os.ReadFile(cardPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(cardContent), "Read the exact section and key", "Reloadmarker: read the exact section and key", 1)
	if updated == string(cardContent) {
		t.Fatal("integration card reload marker was not inserted")
	}
	if err := os.WriteFile(cardPath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := indexpkg.Rebuild(root, derived); err != nil {
		t.Fatal(err)
	}
	sendRPC(t, process.encoder, 5, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "corpus", "query": "reloadmarker"},
	})
	reloadWire := decodeWireToolResult(t, readRPC(t, process.stdout, 5))
	var reloaded search.Response
	if err := json.Unmarshal(reloadWire, &reloaded); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Results) == 0 || reloaded.Results[0].ID != "ini-list-parsing" || !strings.Contains(reloaded.Results[0].Excerpt, "Reloadmarker") {
		t.Fatalf("reloaded search response = %#v", reloaded)
	}

	process.closeAndWait(t)
	if process.stderr.String() != "" {
		t.Fatalf("MCP stderr = %q", process.stderr.String())
	}
}

func TestMCPOneProcessIsolatesNamedProjects(t *testing.T) {
	isolateState(t)
	apiRoot := t.TempDir()
	webRoot := t.TempDir()
	writeTestCard(t, apiRoot, "patterns/first.md", testCard("first-card", "pattern", "active", "# First Card\n\n## Guidance\n\nCrossroot needle guidance for the api service."))
	writeTestCard(t, apiRoot, "patterns/shared.md", testCard("shared-card", "pattern", "active", "# Shared First\n\n## Guidance\n\nCrossroot shared needle guidance."))
	writeTestCard(t, webRoot, "patterns/second.md", testCard("second-card", "pattern", "active", "# Second Card\n\n## Guidance\n\nCrossroot needle guidance for the web service."))
	writeTestCard(t, webRoot, "patterns/shared.md", testCard("shared-card", "pattern", "active", "# Shared Second\n\n## Guidance\n\nCrossroot shared needle guidance."))
	registerProject(t, "api", apiRoot)
	registerProject(t, "web", webRoot)
	for _, id := range []string{"api", "web"} {
		if stdout, stderr, code := execute(t, "index", "--project", id); code != 0 || stderr != "" {
			t.Fatalf("index setup %s: code=%d stdout=%q stderr=%q", id, code, stdout, stderr)
		}
	}

	process := startCLIHelper(t, "mcp", "--project", "api", "--project", "web")
	defer process.cleanup()

	sendRPC(t, process.encoder, 1, "server/discover", map[string]any{"_meta": protocolMeta()})
	readRPC(t, process.stdout, 1)
	sendRPC(t, process.encoder, 2, "tools/list", map[string]any{"_meta": protocolMeta()})
	tools := decodeToolSchemas(t, readRPC(t, process.stdout, 2))
	if len(tools) != 2 {
		t.Fatalf("tools = %#v, want exactly two tools", tools)
	}
	for _, tool := range tools {
		assertProjectEnum(t, tool, []string{"api", "web"})
	}

	sendRPC(t, process.encoder, 3, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "api", "query": "crossroot needle guidance", "limit": 10},
	})
	apiWire := decodeWireToolResult(t, readRPC(t, process.stdout, 3))
	var apiSearch search.Response
	if err := json.Unmarshal(apiWire, &apiSearch); err != nil {
		t.Fatal(err)
	}
	if apiSearch.Project != "api" || apiSearch.Root != apiRoot {
		t.Fatalf("api search envelope = %#v", apiSearch)
	}
	if len(apiSearch.Results) != 2 || apiSearch.Results[0].ID != "first-card" || apiSearch.Results[1].ID != "shared-card" {
		t.Fatalf("api results = %#v, want only api cards", apiSearch.Results)
	}

	sendRPC(t, process.encoder, 4, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "web", "query": "crossroot needle guidance", "limit": 10},
	})
	webWire := decodeWireToolResult(t, readRPC(t, process.stdout, 4))
	var webSearch search.Response
	if err := json.Unmarshal(webWire, &webSearch); err != nil {
		t.Fatal(err)
	}
	if webSearch.Project != "web" || webSearch.Root != webRoot {
		t.Fatalf("web search envelope = %#v", webSearch)
	}
	if len(webSearch.Results) != 2 || webSearch.Results[0].ID != "second-card" || webSearch.Results[1].ID != "shared-card" {
		t.Fatalf("web results = %#v, want only web cards", webSearch.Results)
	}

	sendRPC(t, process.encoder, 5, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_get",
		"arguments": map[string]any{"project": "api", "id": "shared-card"},
	})
	apiGet := decodeWireToolResult(t, readRPC(t, process.stdout, 5))
	var apiCard search.GetResponse
	if err := json.Unmarshal(apiGet, &apiCard); err != nil {
		t.Fatal(err)
	}
	if apiCard.Project != "api" || apiCard.Root != apiRoot || !strings.Contains(apiCard.Card.Content, "Shared First") {
		t.Fatalf("api get = %#v, want the api copy of the colliding card", apiCard)
	}

	sendRPC(t, process.encoder, 6, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_get",
		"arguments": map[string]any{"project": "web", "id": "shared-card"},
	})
	webGet := decodeWireToolResult(t, readRPC(t, process.stdout, 6))
	var webCard search.GetResponse
	if err := json.Unmarshal(webGet, &webCard); err != nil {
		t.Fatal(err)
	}
	if webCard.Project != "web" || webCard.Root != webRoot || !strings.Contains(webCard.Card.Content, "Shared Second") {
		t.Fatalf("web get = %#v, want the web copy of the colliding card", webCard)
	}

	sendRPC(t, process.encoder, 7, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "nope", "query": "crossroot"},
	})
	rejected := readRPC(t, process.stdout, 7)
	text := decodeToolError(t, rejected)
	if !strings.Contains(text, "project") {
		t.Errorf("rejected call text = %q, want a project rejection", text)
	}

	process.closeAndWait(t)
	if stderr := process.stderr.String(); stderr != "" {
		t.Fatalf("MCP stderr = %q, want none for healthy startup", stderr)
	}
}

func TestMCPDegradedProjectStartsWarnsAndRecovers(t *testing.T) {
	isolateState(t)
	apiRoot := t.TempDir()
	webRoot := t.TempDir()
	writeTestCard(t, apiRoot, "patterns/api.md", testCard("api-card", "pattern", "active", "# Api Card\n\n## Guidance\n\nNeedle guidance for api."))
	writeTestCard(t, webRoot, "patterns/web.md", testCard("web-card", "pattern", "active", "# Web Card\n\n## Guidance\n\nNeedle guidance for web."))
	registerProject(t, "api", apiRoot)
	registerProject(t, "web", webRoot)
	if stdout, stderr, code := execute(t, "index", "--project", "api"); code != 0 || stderr != "" {
		t.Fatalf("index setup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestCLIHelperProcess$", "--", "mcp", "--project", "api", "--project", "web")
	command.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	process := startRawProcess(t, command, true)
	defer process.cleanup()

	warning := make(chan string, 1)
	go func() {
		line, _ := process.stderrReader.ReadString('\n')
		warning <- line
	}()
	select {
	case line := <-warning:
		if !strings.Contains(line, `warning: project "web"`) || !strings.Contains(line, "wfm-rag index --project web") {
			t.Fatalf("startup warning = %q", line)
		}
	case <-time.After(protocolResponseLimit):
		t.Fatal("degraded startup warning was not written to stderr")
	}

	sendRPC(t, process.encoder, 1, "server/discover", map[string]any{"_meta": protocolMeta()})
	readRPC(t, process.stdout, 1)
	sendRPC(t, process.encoder, 2, "tools/list", map[string]any{"_meta": protocolMeta()})
	tools := decodeToolSchemas(t, readRPC(t, process.stdout, 2))
	for _, tool := range tools {
		assertProjectEnum(t, tool, []string{"api", "web"})
	}

	sendRPC(t, process.encoder, 3, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "api", "query": "needle guidance"},
	})
	apiWire := decodeWireToolResult(t, readRPC(t, process.stdout, 3))
	var apiSearch search.Response
	if err := json.Unmarshal(apiWire, &apiSearch); err != nil {
		t.Fatal(err)
	}
	if len(apiSearch.Results) == 0 || apiSearch.Results[0].ID != "api-card" {
		t.Fatalf("healthy project call during degraded startup = %#v", apiSearch)
	}

	sendRPC(t, process.encoder, 4, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "web", "query": "needle guidance"},
	})
	degradedText := decodeToolError(t, readRPC(t, process.stdout, 4))
	if !strings.Contains(degradedText, `index_missing: project "web"`) ||
		!strings.Contains(degradedText, "wfm-rag index --project web") ||
		!strings.Contains(degradedText, "Do not retry with a different project") {
		t.Fatalf("degraded call text = %q", degradedText)
	}

	if stdout, stderr, code := execute(t, "index", "--project", "web"); code != 0 || stderr != "" {
		t.Fatalf("repair index: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	sendRPC(t, process.encoder, 5, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "web", "query": "needle guidance"},
	})
	recoveredWire := decodeWireToolResult(t, readRPC(t, process.stdout, 5))
	var recovered search.Response
	if err := json.Unmarshal(recoveredWire, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Project != "web" || len(recovered.Results) == 0 || recovered.Results[0].ID != "web-card" {
		t.Fatalf("recovered call = %#v, want success without a server restart", recovered)
	}

	process.closeAndWait(t)
}

func TestMCPStartupFailureUsesOnlyStderr(t *testing.T) {
	isolateState(t)
	registerProject(t, "api", t.TempDir())
	command := exec.Command(os.Args[0], "-test.run=^TestCLIHelperProcess$", "--", "mcp", "--project", "cloudfire")
	command.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	stdout, err := command.Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("process error = %v, want ExitError", err)
	}
	if exitErr.ExitCode() != 2 || len(stdout) != 0 {
		t.Fatalf("exit=%d stdout=%q", exitErr.ExitCode(), stdout)
	}
	stderr := string(exitErr.Stderr)
	if !strings.Contains(stderr, `project "cloudfire" is not registered`) || !strings.Contains(stderr, "configured projects: api") {
		t.Fatalf("stderr = %q, want project_unknown naming the registry IDs", stderr)
	}
}

func TestFreshCloneEndToEnd(t *testing.T) {
	isolateState(t)
	clone := t.TempDir()
	writeTestCard(t, clone, "patterns/card.md", testCard("card", "pattern", "active", "# Card\n\n## Guidance\n\nNeedle guidance."))

	stdout, stderr, code := execute(t, "project", "init", "api", "--dir", clone)
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "Registered api -> "+clone+"\n") ||
		!strings.Contains(stdout, "Scaffolded 11 knowledge-base files:") {
		t.Fatalf("init: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "index", "--project", "api")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Indexed 1 cards") {
		t.Fatalf("index after scaffold: code=%d stdout=%q stderr=%q; scaffold artifacts must not be indexed", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "search", "--project", "api", "needle")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "1. Card [card]") {
		t.Fatalf("search: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "get", "--project", "api", "card")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "# Card") {
		t.Fatalf("get: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	process := startCLIHelper(t, "mcp", "--project", "api")
	defer process.cleanup()
	sendRPC(t, process.encoder, 1, "server/discover", map[string]any{"_meta": protocolMeta()})
	readRPC(t, process.stdout, 1)
	sendRPC(t, process.encoder, 2, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"project": "api", "query": "needle"},
	})
	wire := decodeWireToolResult(t, readRPC(t, process.stdout, 2))
	var searched search.Response
	if err := json.Unmarshal(wire, &searched); err != nil {
		t.Fatal(err)
	}
	if searched.Project != "api" || len(searched.Results) == 0 || searched.Results[0].ID != "card" {
		t.Fatalf("MCP search = %#v", searched)
	}
	process.closeAndWait(t)
	if process.stderr.String() != "" {
		t.Fatalf("MCP stderr = %q", process.stderr.String())
	}
}

func TestProjectInitScaffoldsEmptyDirectoryIntoWorkingKB(t *testing.T) {
	isolateState(t)
	fresh := t.TempDir()

	stdout, stderr, code := execute(t, "project", "init", "newproj", "--dir", fresh)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Scaffolded 11 knowledge-base files:") {
		t.Fatalf("init: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, rel := range []string{
		".agents/knowledge/README.md",
		".agents/knowledge/schema.md",
		".agents/skills/agent-knowledge/SKILL.md",
	} {
		content, err := os.ReadFile(filepath.Join(fresh, rel))
		if err != nil {
			t.Fatalf("scaffolded %q missing: %v", rel, err)
		}
		if strings.Contains(string(content), "{{PROJECT}}") || !strings.Contains(string(content), "newproj") {
			t.Errorf("scaffolded %q does not name the project ID", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(fresh, ".agents", "knowledge", "templates", "pattern.md")); err != nil {
		t.Errorf("scaffolded template missing: %v", err)
	}

	stdout, stderr, code = execute(t, "index", "--project", "newproj")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Indexed 0 cards") {
		t.Fatalf("index of scaffolded empty corpus: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = execute(t, "search", "--project", "newproj", "anything")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "No results.") {
		t.Fatalf("search after bootstrap: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestStructuredAndTextOutputStayEquivalent(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	writeTestCard(t, root, "patterns/first.md", testCard("first-card", "pattern", "active", "# First Card\n\n## Guidance\n\nNeedle guidance for the first card."))
	writeTestCard(t, root, "patterns/second.md", testCard("second-card", "pattern", "active", "# Second Card\n\n## Guidance\n\nNeedle guidance for the second card."))
	registerProject(t, "api", root)
	if stdout, stderr, code := execute(t, "index", "--project", "api"); code != 0 || stderr != "" {
		t.Fatalf("index setup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	jsonStdout, stderr, code := execute(t, "search", "--project", "api", "--format=json", "needle guidance")
	if code != 0 || stderr != "" {
		t.Fatalf("json search: code=%d stderr=%q", code, stderr)
	}
	var searched search.Response
	if err := json.Unmarshal([]byte(jsonStdout), &searched); err != nil {
		t.Fatal(err)
	}
	textStdout, stderr, code := execute(t, "search", "--project", "api", "needle guidance")
	if code != 0 || stderr != "" {
		t.Fatalf("text search: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(textStdout, "Project: "+searched.Project+"\n") ||
		!strings.Contains(textStdout, "Indexed: "+searched.IndexedAt+" ("+searched.Root+")\n") {
		t.Fatalf("text search header = %q, want the JSON envelope fields", textStdout)
	}
	for position, result := range searched.Results {
		for _, want := range []string{
			fmt.Sprintf("%d. %s [%s] score=%d", position+1, result.Title, result.ID, result.Score),
			"Source: " + result.Source + "#" + result.Heading,
			"Citations: " + strings.Join(result.Citations, ", "),
			"Reasons: " + strings.Join(result.Reasons, "; "),
			result.Excerpt,
		} {
			if !strings.Contains(textStdout, want) {
				t.Errorf("text search output lacks %q for result %#v", want, result)
			}
		}
	}

	jsonStdout, stderr, code = execute(t, "get", "--project", "api", "--format=json", "first-card")
	if code != 0 || stderr != "" {
		t.Fatalf("json get: code=%d stderr=%q", code, stderr)
	}
	var got search.GetResponse
	if err := json.Unmarshal([]byte(jsonStdout), &got); err != nil {
		t.Fatal(err)
	}
	textStdout, stderr, code = execute(t, "get", "--project", "api", "first-card")
	if code != 0 || stderr != "" {
		t.Fatalf("text get: code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Source: " + got.Card.Source,
		"Citations: " + strings.Join(got.Card.Paths, ", "),
		"Indexed: " + got.IndexedAt,
		got.Card.Content,
	} {
		if !strings.Contains(textStdout, want) {
			t.Errorf("text get output lacks %q", want)
		}
	}
}

func TestMCPStdioRequestCancellation(t *testing.T) {
	root := integrationCorpusRoot(t)
	indexPath := filepath.Join(t.TempDir(), "index.json")
	if _, err := indexpkg.Rebuild(root, indexPath); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestMCPBlockingHelperProcess$")
	command.Env = append(os.Environ(), cancelHelperEnv+"=1", cancelHelperRootEnv+"="+root, cancelHelperIndexEnv+"="+indexPath)
	process := startRawProcess(t, command, true)
	defer process.cleanup()

	sendRPC(t, process.encoder, 1, "server/discover", map[string]any{"_meta": protocolMeta()})
	readRPC(t, process.stdout, 1)
	sendRPC(t, process.encoder, 2, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      blockingTestToolName,
		"arguments": map[string]any{},
	})

	started := make(chan string, 1)
	go func() {
		line, _ := process.stderrReader.ReadString('\n')
		started <- line
	}()
	select {
	case line := <-started:
		if line != "handler-started\n" {
			t.Fatalf("helper stderr = %q", line)
		}
	case <-time.After(protocolResponseLimit):
		t.Fatal("blocking tool did not start")
	}

	if err := process.encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/cancelled",
		"params":  map[string]any{"requestId": 2, "reason": "test cancellation"},
	}); err != nil {
		t.Fatal(err)
	}
	cancelled := readRPC(t, process.stdout, 2)
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	decodeRPCResult(t, cancelled, &result)
	if !result.IsError || len(result.Content) != 1 || result.Content[0].Text != context.Canceled.Error() {
		t.Fatalf("cancelled result = %#v", result)
	}
	type readResult struct {
		data []byte
		err  error
	}
	remainingStderr := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(process.stderrReader)
		remainingStderr <- readResult{data: data, err: err}
	}()
	process.closeAndWait(t)
	remaining := <-remainingStderr
	if remaining.err != nil || len(remaining.data) != 0 {
		t.Fatalf("remaining helper stderr = %q, error = %v", remaining.data, remaining.err)
	}
}

func TestMCPBlockingHelperProcess(t *testing.T) {
	if os.Getenv(cancelHelperEnv) != "1" {
		return
	}
	server, _, err := mcpserver.New(context.Background(), []mcpserver.Source{
		{ID: "corpus", Root: os.Getenv(cancelHelperRootEnv), IndexPath: os.Getenv(cancelHelperIndexEnv)},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mcp.AddTool(server, &mcp.Tool{Name: blockingTestToolName}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
		fmt.Fprintln(os.Stderr, "handler-started")
		<-ctx.Done()
		return nil, struct{}{}, ctx.Err()
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

type rawMCPProcess struct {
	command      *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Reader
	stderr       bytes.Buffer
	stderrReader *bufio.Reader
	encoder      *json.Encoder
	waited       bool
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type toolSchema struct {
	Name        string
	InputSchema map[string]any
}

func startCLIHelper(t *testing.T, args ...string) *rawMCPProcess {
	t.Helper()
	commandArgs := []string{"-test.run=^TestCLIHelperProcess$", "--"}
	commandArgs = append(commandArgs, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return startRawProcess(t, command, false)
}

func startRawProcess(t *testing.T, command *exec.Cmd, streamStderr bool) *rawMCPProcess {
	t.Helper()
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	process := &rawMCPProcess{command: command, stdin: stdin}
	if command.Stderr == nil {
		if streamStderr {
			stderr, err := command.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			process.stderrReader = bufio.NewReader(stderr)
		} else {
			command.Stderr = &process.stderr
		}
	}
	process.stdout = bufio.NewReader(stdout)
	process.encoder = json.NewEncoder(stdin)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	return process
}

func (process *rawMCPProcess) cleanup() {
	if process.waited {
		return
	}
	process.stdin.Close()
	if process.command.Process != nil {
		process.command.Process.Kill()
	}
	process.command.Wait()
}

func (process *rawMCPProcess) closeAndWait(t *testing.T) {
	t.Helper()
	if err := process.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	extraOutput := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(process.stdout)
		extraOutput <- data
	}()
	waited := make(chan error, 1)
	go func() {
		waited <- process.command.Wait()
	}()
	select {
	case err := <-waited:
		process.waited = true
		if err != nil {
			t.Fatalf("MCP process exit: %v", err)
		}
	case <-time.After(protocolResponseLimit):
		process.command.Process.Kill()
		<-waited
		process.waited = true
		t.Fatal("MCP process did not exit after stdin EOF")
	}
	if extra := <-extraOutput; len(bytes.TrimSpace(extra)) != 0 {
		for _, line := range bytes.Split(bytes.TrimSpace(extra), []byte{'\n'}) {
			if !json.Valid(line) {
				t.Fatalf("non-JSON MCP stdout line: %q", line)
			}
		}
		t.Fatalf("unexpected extra MCP stdout: %s", extra)
	}
}

func sendRPC(t *testing.T, encoder *json.Encoder, id int, method string, params map[string]any) {
	t.Helper()
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		t.Fatal(err)
	}
}

func readRPC(t *testing.T, reader *bufio.Reader, wantID int) rpcEnvelope {
	t.Helper()
	type lineResult struct {
		line []byte
		err  error
	}
	read := make(chan lineResult, 1)
	go func() {
		line, err := readBoundedProtocolLine(reader)
		read <- lineResult{line: line, err: err}
	}()
	var line []byte
	select {
	case result := <-read:
		if result.err != nil {
			t.Fatalf("read MCP response: %v", result.err)
		}
		line = result.line
	case <-time.After(protocolResponseLimit):
		t.Fatalf("timed out waiting for MCP response %d", wantID)
	}
	var response rpcEnvelope
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("invalid MCP stdout line: %v\n%s", err, line)
	}
	if response.JSONRPC != "2.0" || response.ID != wantID || len(response.Error) != 0 {
		t.Fatalf("MCP response = %#v", response)
	}
	return response
}

func readBoundedProtocolLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxProtocolLineBytes {
			return nil, fmt.Errorf("MCP response exceeds %d bytes", maxProtocolLineBytes)
		}
		line = append(line, fragment...)
		if err == nil {
			return line, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}

func decodeRPCResult(t *testing.T, response rpcEnvelope, target any) {
	t.Helper()
	if len(response.Result) == 0 {
		t.Fatal("MCP response has no result")
	}
	if err := json.Unmarshal(response.Result, target); err != nil {
		t.Fatalf("decode MCP result: %v\n%s", err, response.Result)
	}
}

func decodeWireToolResult(t *testing.T, response rpcEnvelope) json.RawMessage {
	t.Helper()
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	decodeRPCResult(t, response, &result)
	if result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" || len(result.StructuredContent) == 0 {
		t.Fatalf("tool result = %#v", result)
	}
	var fallback, structured any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &fallback); err != nil {
		t.Fatalf("invalid text fallback: %v", err)
	}
	if err := json.Unmarshal(result.StructuredContent, &structured); err != nil {
		t.Fatalf("invalid structured content: %v", err)
	}
	if !reflect.DeepEqual(fallback, structured) {
		t.Fatalf("text fallback differs from structured content")
	}
	return result.StructuredContent
}

func decodeToolError(t *testing.T, response rpcEnvelope) string {
	t.Helper()
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	decodeRPCResult(t, response, &result)
	if !result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("expected a tool error result, got %#v", result)
	}
	return result.Content[0].Text
}

func decodeToolSchemas(t *testing.T, response rpcEnvelope) []toolSchema {
	t.Helper()
	var listed struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	decodeRPCResult(t, response, &listed)
	tools := make([]toolSchema, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		tools = append(tools, toolSchema{Name: tool.Name, InputSchema: tool.InputSchema})
	}
	return tools
}

// assertProjectEnum checks the closed-world project contract of one tool schema.
func assertProjectEnum(t *testing.T, tool toolSchema, wantEnum []string) {
	t.Helper()
	if tool.InputSchema["additionalProperties"] != false {
		t.Errorf("tool %q is not closed-world: %#v", tool.Name, tool.InputSchema)
	}
	required, _ := tool.InputSchema["required"].([]any)
	found := false
	for _, value := range required {
		if value == "project" {
			found = true
		}
	}
	if !found {
		t.Errorf("tool %q does not require project: %#v", tool.Name, tool.InputSchema["required"])
	}
	properties, _ := tool.InputSchema["properties"].(map[string]any)
	projectSchema, ok := properties["project"].(map[string]any)
	if !ok {
		t.Fatalf("tool %q has no project property: %#v", tool.Name, properties)
	}
	enum, _ := projectSchema["enum"].([]any)
	got := make([]string, 0, len(enum))
	for _, value := range enum {
		got = append(got, value.(string))
	}
	if !reflect.DeepEqual(got, wantEnum) {
		t.Errorf("tool %q project enum = %v, want %v", tool.Name, got, wantEnum)
	}
	for _, forbidden := range []string{"root", "index", "path", "write", "shell"} {
		if properties[forbidden] != nil {
			t.Errorf("tool %q exposes forbidden property %q", tool.Name, forbidden)
		}
	}
}

func protocolMeta() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/clientCapabilities": map[string]any{"roots": map[string]any{"listChanged": true}},
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "wfm-rag-test", "version": "0.1.0"},
		"io.modelcontextprotocol/protocolVersion":    protocolVersion,
	}
}

func integrationCorpusRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "internal", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func mutableIntegrationCorpusRoot(t *testing.T) string {
	t.Helper()
	source := integrationCorpusRoot(t)
	root := t.TempDir()
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("fixture %q is not a regular file", path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func connectIntegrationClient(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "wfm-rag-integration-test", Version: "0.1.0"}, nil)
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

func decodeMCPStructured(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
}
