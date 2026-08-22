// Package mcpserver exposes the read-only knowledge retrieval tools over MCP.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	indexpkg "wfm-rag/internal/index"
	"wfm-rag/internal/search"
)

const (
	serverName    = "wfm-rag"
	serverVersion = "0.1.0"

	searchToolName = "agent_knowledge_search"
	getToolName    = "agent_knowledge_get"
)

// ErrReload identifies a changed index that could not be loaded safely.
var ErrReload = errors.New("index reload failure")

// GetInput identifies one card for exact retrieval.
type GetInput struct {
	ID string `json:"id" jsonschema:"stable, case-sensitive card ID"`
}

type indexState struct {
	mu          sync.Mutex
	root        string
	path        string
	value       *indexpkg.Index
	fingerprint indexFingerprint
}

type indexFingerprint struct {
	info os.FileInfo
}

// New loads the configured index and constructs a server with the two
// read-only knowledge tools.
func New(root, indexPath string) (*mcp.Server, error) {
	return newServer(context.Background(), root, indexPath)
}

func newServer(ctx context.Context, root, indexPath string) (*mcp.Server, error) {
	value, fingerprint, err := loadStable(ctx, indexPath, root)
	if err != nil {
		return nil, err
	}
	state := &indexState{
		root:        value.Root,
		path:        indexPath,
		value:       value,
		fingerprint: fingerprint,
	}

	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	openWorld := false
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld}
	mcp.AddTool(server, &mcp.Tool{
		Name:        searchToolName,
		Title:       "Search agent knowledge",
		Description: "Search active knowledge cards in the server's fixed repository index using deterministic ranking and optional citation filters.",
		Annotations: annotations,
	}, state.search)
	mcp.AddTool(server, &mcp.Tool{
		Name:        getToolName,
		Title:       "Get agent knowledge card",
		Description: "Return one complete indexed knowledge card by its stable ID, including metadata, Markdown content, source path, and citations.",
		Annotations: annotations,
	}, state.get)
	return server, nil
}

// Run serves one MCP session until the transport closes or ctx is cancelled.
func Run(ctx context.Context, root, indexPath string, transport mcp.Transport) error {
	server, err := newServer(ctx, root, indexPath)
	if err != nil {
		return err
	}
	return server.Run(ctx, transport)
}

func (state *indexState) search(ctx context.Context, _ *mcp.CallToolRequest, query search.Query) (*mcp.CallToolResult, search.Response, error) {
	if err := search.ValidateQuery(query); err != nil {
		return nil, search.Response{}, err
	}
	value, err := state.current(ctx)
	if err != nil {
		return nil, search.Response{}, err
	}
	response, err := search.Search(value, query)
	if err != nil {
		return nil, search.Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, search.Response{}, err
	}
	return nil, response, nil
}

func (state *indexState) get(ctx context.Context, _ *mcp.CallToolRequest, input GetInput) (*mcp.CallToolResult, search.GetResponse, error) {
	if err := search.ValidateID(input.ID); err != nil {
		return nil, search.GetResponse{}, err
	}
	value, err := state.current(ctx)
	if err != nil {
		return nil, search.GetResponse{}, err
	}
	response, err := search.Get(value, input.ID)
	if err != nil {
		return nil, search.GetResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, search.GetResponse{}, err
	}
	return nil, response, nil
}

func (state *indexState) current(ctx context.Context) (*indexpkg.Index, error) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fingerprint, err := statIndex(state.path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReload, err)
	}
	if fingerprint.equal(state.fingerprint) {
		return state.value, nil
	}

	value, fingerprint, err := loadStable(ctx, state.path, state.root)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrReload, err)
	}
	state.value = value
	state.fingerprint = fingerprint
	return value, nil
}

func loadStable(ctx context.Context, path, root string) (*indexpkg.Index, indexFingerprint, error) {
	if err := ctx.Err(); err != nil {
		return nil, indexFingerprint{}, err
	}
	before, err := statIndex(path)
	if err != nil {
		return nil, indexFingerprint{}, err
	}
	value, err := indexpkg.Load(path, root)
	if err != nil {
		return nil, indexFingerprint{}, err
	}
	after, err := statIndex(path)
	if err != nil {
		return nil, indexFingerprint{}, err
	}
	if !before.equal(after) {
		return nil, indexFingerprint{}, fmt.Errorf("%w: index %q changed while loading", indexpkg.ErrInvalid, path)
	}
	if err := ctx.Err(); err != nil {
		return nil, indexFingerprint{}, err
	}
	return value, after, nil
}

func statIndex(path string) (indexFingerprint, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return indexFingerprint{}, fmt.Errorf("%w: %q", indexpkg.ErrMissing, path)
		}
		return indexFingerprint{}, fmt.Errorf("inspect index %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return indexFingerprint{}, fmt.Errorf("%w: index %q is not a regular file", indexpkg.ErrInvalid, path)
	}
	return indexFingerprint{info: info}, nil
}

func (fingerprint indexFingerprint) equal(other indexFingerprint) bool {
	return fingerprint.info.Size() == other.info.Size() &&
		fingerprint.info.ModTime().Equal(other.info.ModTime()) &&
		os.SameFile(fingerprint.info, other.info)
}
