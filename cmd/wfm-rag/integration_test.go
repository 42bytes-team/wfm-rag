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
	root := integrationCorpusRoot(t)
	indexPath := filepath.Join(t.TempDir(), "index.json")
	if _, err := indexpkg.Rebuild(root, indexPath); err != nil {
		t.Fatal(err)
	}

	server, err := mcpserver.New(root, indexPath)
	if err != nil {
		t.Fatal(err)
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
			stdout, stderr, code := execute(t, "search", "--root", root, "--index", indexPath, "--format=json", test.query)
			if code != 0 || stderr != "" {
				t.Fatalf("CLI search code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var cliResponse search.Response
			if err := json.Unmarshal([]byte(stdout), &cliResponse); err != nil {
				t.Fatal(err)
			}
			mcpResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "agent_knowledge_search",
				Arguments: map[string]any{"query": test.query},
			})
			if err != nil || mcpResult.IsError {
				t.Fatalf("MCP search result=%#v error=%v", mcpResult, err)
			}
			var mcpResponse search.Response
			decodeMCPStructured(t, mcpResult, &mcpResponse)
			if !reflect.DeepEqual(mcpResponse, cliResponse) {
				t.Fatalf("MCP response differs from CLI\nMCP: %#v\nCLI: %#v", mcpResponse, cliResponse)
			}
			if len(cliResponse.Results) == 0 || cliResponse.Results[0].ID != test.want {
				t.Fatalf("results = %#v, want %q first", cliResponse.Results, test.want)
			}
		})
	}

	stdout, stderr, code := execute(t, "get", "--root", root, "--index", indexPath, "--format=json", "ini-list-parsing")
	if code != 0 || stderr != "" {
		t.Fatalf("CLI get code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var cliGet search.GetResponse
	if err := json.Unmarshal([]byte(stdout), &cliGet); err != nil {
		t.Fatal(err)
	}
	mcpResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "agent_knowledge_get",
		Arguments: map[string]any{"id": "ini-list-parsing"},
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
	root := mutableIntegrationCorpusRoot(t)
	indexPath := filepath.Join(t.TempDir(), "index.json")
	if _, err := indexpkg.Rebuild(root, indexPath); err != nil {
		t.Fatal(err)
	}

	process := startCLIHelper(t, "mcp", "--root", root, "--index", indexPath)
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
	var tools struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	decodeRPCResult(t, listed, &tools)
	if len(tools.Tools) != 2 || tools.Tools[0].Name != "agent_knowledge_get" || tools.Tools[1].Name != "agent_knowledge_search" {
		t.Fatalf("tools = %#v", tools.Tools)
	}
	for _, tool := range tools.Tools {
		properties, _ := tool.InputSchema["properties"].(map[string]any)
		if tool.InputSchema["additionalProperties"] != false || properties["root"] != nil || properties["project"] != nil {
			t.Errorf("unsafe input schema for %q: %#v", tool.Name, tool.InputSchema)
		}
	}

	sendRPC(t, process.encoder, 3, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"query": "config parser comma separated"},
	})
	searchWire := decodeWireToolResult(t, readRPC(t, process.stdout, 3))
	var searched search.Response
	if err := json.Unmarshal(searchWire, &searched); err != nil {
		t.Fatal(err)
	}
	if len(searched.Results) == 0 || searched.Results[0].ID != "ini-list-parsing" {
		t.Fatalf("search response = %#v", searched)
	}

	sendRPC(t, process.encoder, 4, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_get",
		"arguments": map[string]any{"id": "ini-list-parsing"},
	})
	getWire := decodeWireToolResult(t, readRPC(t, process.stdout, 4))
	var got search.GetResponse
	if err := json.Unmarshal(getWire, &got); err != nil {
		t.Fatal(err)
	}
	if got.Card.ID != "ini-list-parsing" || !strings.Contains(got.Card.Content, `Key.Strings(",")`) {
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
	if _, err := indexpkg.Rebuild(root, indexPath); err != nil {
		t.Fatal(err)
	}
	sendRPC(t, process.encoder, 5, "tools/call", map[string]any{
		"_meta":     protocolMeta(),
		"name":      "agent_knowledge_search",
		"arguments": map[string]any{"query": "reloadmarker"},
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

func TestMCPStartupFailureUsesOnlyStderr(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.json")
	command := exec.Command(os.Args[0], "-test.run=^TestCLIHelperProcess$", "--", "mcp", "--root", root, "--index", missing)
	command.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	stdout, err := command.Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("process error = %v, want ExitError", err)
	}
	if exitErr.ExitCode() != 1 || len(stdout) != 0 || !strings.Contains(string(exitErr.Stderr), "index missing") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitErr.ExitCode(), stdout, exitErr.Stderr)
	}
}

func TestMCPBlockingHelperProcess(t *testing.T) {
	if os.Getenv(cancelHelperEnv) != "1" {
		return
	}
	server, err := mcpserver.New(os.Getenv(cancelHelperRootEnv), os.Getenv(cancelHelperIndexEnv))
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
		t.Fatal(err)
	}
}
