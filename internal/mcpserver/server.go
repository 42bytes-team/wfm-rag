// Package mcpserver exposes the read-only knowledge retrieval tools over MCP.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wfm-rag/internal/index"
	"wfm-rag/internal/search"
)

const (
	serverName    = "wfm-rag"
	serverVersion = "0.3.0"

	searchToolName = "agent_knowledge_search"
	getToolName    = "agent_knowledge_get"
)

// ErrReload identifies a configured project whose index changed and could not
// be loaded safely, or a degraded project whose index is still missing or invalid.
var ErrReload = errors.New("index reload failure")

// Source identifies one configured project index. ID is the launch-allowlist
// name that tool inputs must select.
type Source struct {
	ID        string
	Root      string
	IndexPath string
}

// LoadWarning reports a configured project whose initial index load failed.
// New still constructs the server; calls that select that project report the
// problem until a later call finds a valid index.
type LoadWarning struct {
	Project string
	Path    string
	Err     error
}

// SearchInput is the closed-world search envelope. The caller names exactly one
// configured project and never a root, index path, or write operation.
type SearchInput struct {
	Project  string   `json:"project" jsonschema:"configured project ID to search"`
	Query    string   `json:"query" jsonschema:"search text"`
	Paths    []string `json:"paths,omitempty" jsonschema:"cited path filters"`
	Packages []string `json:"packages,omitempty" jsonschema:"package filters"`
	Types    []string `json:"types,omitempty" jsonschema:"card type filters"`
	Limit    *int     `json:"limit,omitempty" jsonschema:"result limit, 1 to 20"`
}

// GetInput identifies one card inside one configured project.
type GetInput struct {
	Project string `json:"project" jsonschema:"configured project ID"`
	ID      string `json:"id" jsonschema:"stable, case-sensitive card ID"`
}

type projectState struct {
	id          string
	root        string
	path        string
	value       *index.Index
	fingerprint indexFingerprint
}

type serverState struct {
	mu       sync.Mutex
	projects map[string]*projectState
}

type indexFingerprint struct {
	info os.FileInfo
}

// New constructs the server with the two read-only knowledge tools. Each
// source's index is attempted at startup; a failed load degrades only that
// project and yields a warning for the caller to print on stderr. The project
// enum published in both tool schemas is exactly the configured IDs, sorted.
func New(ctx context.Context, sources []Source) (*mcp.Server, []LoadWarning, error) {
	if len(sources) == 0 {
		return nil, nil, errors.New("construct server: at least one source is required")
	}
	state := &serverState{projects: make(map[string]*projectState, len(sources))}
	warnings := []LoadWarning{}
	for _, source := range sources {
		if strings.TrimSpace(source.ID) == "" {
			return nil, nil, errors.New("construct server: source project ID is required")
		}
		if _, duplicate := state.projects[source.ID]; duplicate {
			return nil, nil, fmt.Errorf("construct server: project %q is configured more than once", source.ID)
		}
		project := &projectState{id: source.ID, root: source.Root, path: source.IndexPath}
		value, fingerprint, err := loadStable(ctx, source.IndexPath, source.Root)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil, err
			}
			warnings = append(warnings, LoadWarning{Project: source.ID, Path: source.IndexPath, Err: err})
		} else {
			project.value = value
			project.fingerprint = fingerprint
		}
		state.projects[source.ID] = project
	}

	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	openWorld := false
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld}
	enum := state.sortedIDs()

	searchSchema, err := projectEnumSchema[SearchInput](enum)
	if err != nil {
		return nil, nil, fmt.Errorf("construct search tool schema: %w", err)
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        searchToolName,
		Title:       "Search agent knowledge",
		Description: "Search active knowledge cards in exactly one named project index using deterministic ranking and optional citation filters. The project field must be one of the configured project IDs.",
		Annotations: annotations,
		InputSchema: searchSchema,
	}, state.search)

	getSchema, err := projectEnumSchema[GetInput](enum)
	if err != nil {
		return nil, nil, fmt.Errorf("construct get tool schema: %w", err)
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        getToolName,
		Title:       "Get agent knowledge card",
		Description: "Return one complete knowledge card by its stable ID from exactly one named project index. The project field must be one of the configured project IDs.",
		Annotations: annotations,
		InputSchema: getSchema,
	}, state.get)

	return server, warnings, nil
}

func projectEnumSchema[T any](enum []string) (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, err
	}
	project, ok := schema.Properties["project"]
	if !ok {
		return nil, errors.New("tool schema has no project property")
	}
	values := make([]any, 0, len(enum))
	for _, id := range enum {
		values = append(values, id)
	}
	project.Enum = values
	return schema, nil
}

func (state *serverState) search(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, search.Response, error) {
	query := search.Query{
		Text:     input.Query,
		Paths:    input.Paths,
		Packages: input.Packages,
		Types:    input.Types,
		Limit:    input.Limit,
	}
	if err := search.ValidateQuery(query); err != nil {
		return nil, search.Response{}, err
	}
	value, err := state.currentProject(ctx, input.Project)
	if err != nil {
		return nil, search.Response{}, err
	}
	response, err := search.Search(value, input.Project, query)
	if err != nil {
		return nil, search.Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, search.Response{}, err
	}
	return nil, response, nil
}

func (state *serverState) get(ctx context.Context, _ *mcp.CallToolRequest, input GetInput) (*mcp.CallToolResult, search.GetResponse, error) {
	if err := search.ValidateID(input.ID); err != nil {
		return nil, search.GetResponse{}, err
	}
	value, err := state.currentProject(ctx, input.Project)
	if err != nil {
		return nil, search.GetResponse{}, err
	}
	response, err := search.Get(value, input.Project, input.ID)
	if err != nil {
		return nil, search.GetResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, search.GetResponse{}, err
	}
	return nil, response, nil
}

// currentProject returns the validated index for exactly one selected project.
// A selected project whose file changed reloads before use. Failures are
// scoped to the selected project: another project's broken index never fails
// this call, and a degraded project recovers as soon as a valid index exists.
func (state *serverState) currentProject(ctx context.Context, id string) (*index.Index, error) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := state.projects[id]
	if !ok {
		return nil, fmt.Errorf("project_unknown: project %q is not configured for this server; configured projects: %s",
			id, strings.Join(state.sortedIDs(), ", "))
	}
	fingerprint, err := statIndex(project.path)
	if err != nil {
		if project.value != nil {
			// The file vanished after a successful startup load.
			return nil, degradedError(id, project.path, err)
		}
		return nil, degradedError(id, project.path, err)
	}
	value := project.value
	if value == nil || !fingerprint.equal(project.fingerprint) {
		value, fingerprint, err = loadStable(ctx, project.path, project.root)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			project.value = nil
			return nil, degradedError(id, project.path, err)
		}
		project.value = value
		project.fingerprint = fingerprint
	}
	return value, nil
}

// degradedError is written for the model that will see it first: it names the
// project, the repair command, and forbids guessing another project.
func degradedError(id, path string, cause error) error {
	return fmt.Errorf("%w: index_missing: project %q has no valid index at %q (%v). Ask the operator to run: wfm-rag index --project %s. Do not retry with a different project",
		ErrReload, id, path, cause, id)
}

func (state *serverState) sortedIDs() []string {
	ids := make([]string, 0, len(state.projects))
	for id := range state.projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func loadStable(ctx context.Context, path, root string) (*index.Index, indexFingerprint, error) {
	if err := ctx.Err(); err != nil {
		return nil, indexFingerprint{}, err
	}
	before, err := statIndex(path)
	if err != nil {
		return nil, indexFingerprint{}, err
	}
	value, err := index.Load(path, root)
	if err != nil {
		return nil, indexFingerprint{}, err
	}
	after, err := statIndex(path)
	if err != nil {
		return nil, indexFingerprint{}, err
	}
	if !before.equal(after) {
		return nil, indexFingerprint{}, fmt.Errorf("%w: index %q changed while loading", index.ErrInvalid, path)
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
			return indexFingerprint{}, fmt.Errorf("%w: %q", index.ErrMissing, path)
		}
		return indexFingerprint{}, fmt.Errorf("inspect index %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return indexFingerprint{}, fmt.Errorf("%w: index %q is not a regular file", index.ErrInvalid, path)
	}
	return indexFingerprint{info: info}, nil
}

func (fingerprint indexFingerprint) equal(other indexFingerprint) bool {
	return fingerprint.info.Size() == other.info.Size() &&
		fingerprint.info.ModTime().Equal(other.info.ModTime()) &&
		os.SameFile(fingerprint.info, other.info)
}
