package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wfm-rag/internal/cards"
	indexpkg "wfm-rag/internal/index"
	"wfm-rag/internal/mcpserver"
	"wfm-rag/internal/project"
	"wfm-rag/internal/scaffold"
	"wfm-rag/internal/search"
)

const usage = `usage:
  wfm-rag index --project <id> [--index <file>] [--format text|json]
  wfm-rag search --project <id> [--index <file>] [--path <path>] [--package <pkg>] [--type <type>] [--limit 5] [--format text|json] <query>
  wfm-rag get --project <id> [--index <file>] [--format text|json] <card-id>
  wfm-rag mcp --project <id> [--project <id> ...] | --all-projects [--format text|json]
  wfm-rag project init <id> [--dir <path>] [--format text|json]
  wfm-rag project remove <id> [--format text|json]
  wfm-rag project list [--format text|json]
  wfm-rag project healthcheck [--verify] [--purge] [--format text|json]`

// maxConfiguredProjects bounds how many projects one MCP process may serve.
const maxConfiguredProjects = project.MaxProjects

// errRootMissing labels a registered project whose canonical root no longer exists.
var errRootMissing = errors.New("project root missing")

// projectIDPattern mirrors the registry owner's ID rule so a lookup failure can
// distinguish a malformed ID from an unregistered one before any index is touched.
var projectIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

type outputFormat string

const (
	formatText outputFormat = "text"
	formatJSON outputFormat = "json"
)

type commandError struct {
	code    string
	message string
	cause   error
	usage   bool
}

func (err *commandError) Error() string {
	return err.message
}

func (err *commandError) Unwrap() error {
	return err.cause
}

type errorOutput struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type indexOutput struct {
	Root        string          `json:"root"`
	IndexedAt   time.Time       `json:"indexed_at"`
	Cards       int             `json:"cards"`
	Skipped     int             `json:"skipped"`
	Chunks      int             `json:"chunks"`
	SourceBytes int64           `json:"source_bytes"`
	Warnings    []cards.Warning `json:"warnings"`
	Index       string          `json:"index"`
}

type projectMutationOutput struct {
	Project      string   `json:"project"`
	Root         string   `json:"root"`
	PreviousRoot string   `json:"previous_root,omitempty"`
	Scaffolded   []string `json:"scaffolded,omitempty"`
	Rebound      bool     `json:"rebound"`
	Created      bool     `json:"created"`
}

type projectsOutput struct {
	Projects []project.Status `json:"projects"`
}

type healthcheckOutput struct {
	Findings []project.Finding `json:"findings"`
	Purged   []string          `json:"purged"`
}

type commonFlags struct {
	project singleFlag
	index   singleFlag
	format  string
}

// singleFlag accepts a value exactly once so retired repeatable flags cannot
// silently reappear as repeated single flags.
type singleFlag struct {
	name  string
	value string
	set   bool
}

func (value *singleFlag) String() string {
	return value.value
}

func (value *singleFlag) Set(raw string) error {
	if value.set {
		return fmt.Errorf("%s may be given once", value.name)
	}
	value.value = raw
	value.set = true
	return nil
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type optionalInt struct {
	value int
	set   bool
}

func (value *optionalInt) String() string {
	return fmt.Sprintf("%d", value.value)
}

func (value *optionalInt) Set(raw string) error {
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("invalid integer %q", raw)
	}
	value.value = parsed
	value.set = true
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	format := formatText
	if len(args) == 0 {
		return writeFailure(stderr, format, invalidArguments("a command is required", true))
	}

	var err error
	switch args[0] {
	case "index":
		err = runIndex(args[1:], stdout, &format)
	case "search":
		err = runSearch(args[1:], stdout, &format)
	case "get":
		err = runGet(args[1:], stdout, &format)
	case "mcp":
		err = runMCP(args[1:], stderr, &format)
	case "project":
		err = runProject(args[1:], stdout, &format)
	case "help", "-h", "--help":
		_, err = fmt.Fprintln(stdout, usage)
		if err != nil {
			err = classifyError(fmt.Errorf("write help: %w", err))
		}
	default:
		err = invalidArguments(fmt.Sprintf("unknown command %q", args[0]), true)
	}
	if err == nil {
		return 0
	}
	return writeFailure(stderr, format, err)
}

func runMCP(args []string, stderr io.Writer, failureFormat *outputFormat) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var projects stringList
	flags.Var(&projects, "project", "registered project ID to serve (repeatable)")
	allProjects := flags.Bool("all-projects", false, "serve every registered project")
	format := flags.String("format", string(formatText), "startup error format")
	if err := flags.Parse(args); err != nil {
		*failureFormat = parsedFormat(*format)
		return invalidArguments(err.Error(), true)
	}
	*failureFormat = parsedFormat(*format)
	if flags.NArg() != 0 {
		return invalidArguments(fmt.Sprintf("expected 0 positional argument, got %d", flags.NArg()), true)
	}
	if len(projects) > maxConfiguredProjects {
		return &commandError{
			code:    "project_limit_exceeded",
			message: fmt.Sprintf("--project may be given at most %d times", maxConfiguredProjects),
			cause:   project.ErrLimitExceeded,
			usage:   true,
		}
	}
	if len(projects) > 0 && *allProjects {
		return invalidArguments("--all-projects cannot be combined with --project", true)
	}
	if len(projects) == 0 && !*allProjects {
		return projectRequired("--project is required; or use --all-projects")
	}
	registry, err := project.Load()
	if err != nil {
		return classifyError(err)
	}
	ids := make([]string, 0, len(projects))
	if *allProjects {
		ids = sortedRegistryIDs(registry)
		if len(ids) == 0 {
			return projectRequired("no projects are registered")
		}
	} else {
		seen := make(map[string]bool, len(projects))
		for _, id := range projects {
			if !projectIDPattern.MatchString(id) {
				return projectIDInvalid(id)
			}
			if _, registered := registry.Projects[id]; !registered {
				return projectUnknown(id, registry)
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sources := make([]mcpserver.Source, 0, len(ids))
	for _, id := range ids {
		root := registry.Projects[id].Root
		path, err := indexpkg.DefaultPathFromCanonical(root)
		if err != nil {
			return classifyError(err)
		}
		sources = append(sources, mcpserver.Source{ID: id, Root: root, IndexPath: path})
	}
	server, warnings, err := mcpserver.New(context.Background(), sources)
	if err != nil {
		return classifyError(err)
	}
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(stderr, "warning: project %q: no valid index at %s; calls selecting it will fail until `wfm-rag index --project %s` succeeds: %v\n",
			warning.Project, warning.Path, warning.Project, warning.Err); err != nil {
			return classifyError(fmt.Errorf("write mcp diagnostics: %w", err))
		}
	}
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

func runIndex(args []string, stdout io.Writer, failureFormat *outputFormat) error {
	flags, common := newFlagSet("index")
	if err := flags.Parse(args); err != nil {
		*failureFormat = parsedFormat(common.format)
		return invalidArguments(err.Error(), true)
	}
	*failureFormat = parsedFormat(common.format)
	format, err := validateCommon(common, flags.NArg(), 0)
	if err != nil {
		return err
	}
	root, err := resolveProject(common.project.value)
	if err != nil {
		return err
	}
	path, err := resolveIndexPath(root, common.index.value)
	if err != nil {
		return classifyError(err)
	}
	result, err := indexpkg.Rebuild(root, path)
	if err != nil {
		return classifyError(err)
	}
	output := indexOutput{
		Root:        result.Index.Root,
		IndexedAt:   result.Index.GeneratedAt,
		Cards:       len(result.Index.Documents),
		Skipped:     result.Skipped,
		Chunks:      len(result.Index.Chunks),
		SourceBytes: result.SourceBytes,
		Warnings:    result.Warnings,
		Index:       result.Path,
	}
	if format == formatJSON {
		return writeJSON(stdout, output)
	}
	if _, err := fmt.Fprintf(stdout, "Indexed %d cards and %d chunks\nSkipped: %d\nSource bytes: %d\nIndex: %s\nGenerated: %s\n",
		output.Cards, output.Chunks, output.Skipped, output.SourceBytes, output.Index, output.IndexedAt.Format(time.RFC3339Nano)); err != nil {
		return classifyError(fmt.Errorf("write index output: %w", err))
	}
	for _, warning := range output.Warnings {
		cardID := ""
		if warning.CardID != "" {
			cardID = " (" + warning.CardID + ")"
		}
		if _, err := fmt.Fprintf(stdout, "Warning: %s%s: %s\n", warning.Source, cardID, warning.Message); err != nil {
			return classifyError(fmt.Errorf("write index output: %w", err))
		}
	}
	return nil
}

func runSearch(args []string, stdout io.Writer, failureFormat *outputFormat) error {
	flags, common := newFlagSet("search")
	var paths, packages, types stringList
	limit := optionalInt{value: search.DefaultLimit}
	flags.Var(&paths, "path", "cited path filter (repeatable)")
	flags.Var(&packages, "package", "package filter (repeatable)")
	flags.Var(&types, "type", "card type filter (repeatable)")
	flags.Var(&limit, "limit", "result limit")
	if err := flags.Parse(args); err != nil {
		*failureFormat = parsedFormat(common.format)
		return invalidArguments(err.Error(), true)
	}
	*failureFormat = parsedFormat(common.format)
	format, err := validateCommon(common, flags.NArg(), 1)
	if err != nil {
		return err
	}
	query := search.Query{Text: flags.Arg(0), Paths: paths, Packages: packages, Types: types}
	if limit.set {
		query.Limit = &limit.value
	}
	if err := search.ValidateQuery(query); err != nil {
		return invalidArguments(err.Error(), true)
	}
	root, err := resolveProject(common.project.value)
	if err != nil {
		return err
	}
	path, err := resolveIndexPath(root, common.index.value)
	if err != nil {
		return classifyError(err)
	}
	value, err := indexpkg.Load(path, root)
	if err != nil {
		return retrievalIndexFailure(common.project.value, path, err)
	}
	response, err := search.Search(value, common.project.value, query)
	if err != nil {
		return classifyError(err)
	}
	if format == formatJSON {
		return writeJSON(stdout, response)
	}
	if _, err := fmt.Fprintf(stdout, "Project: %s\nIndexed: %s (%s)\n", response.Project, response.IndexedAt, response.Root); err != nil {
		return classifyError(fmt.Errorf("write search output: %w", err))
	}
	if len(response.Results) == 0 {
		if _, err := fmt.Fprintln(stdout, "No results."); err != nil {
			return classifyError(fmt.Errorf("write search output: %w", err))
		}
		return nil
	}
	for position, result := range response.Results {
		if position > 0 {
			if _, err := fmt.Fprintln(stdout); err != nil {
				return classifyError(fmt.Errorf("write search output: %w", err))
			}
		}
		if _, err := fmt.Fprintf(stdout, "%d. %s [%s] score=%d\nType: %s\nSource: %s#%s\nCitations: %s\nReasons: %s\n%s\n",
			position+1, result.Title, result.ID, result.Score, result.Type, result.Source, result.Heading,
			strings.Join(result.Citations, ", "), strings.Join(result.Reasons, "; "), result.Excerpt); err != nil {
			return classifyError(fmt.Errorf("write search output: %w", err))
		}
	}
	return nil
}

func runGet(args []string, stdout io.Writer, failureFormat *outputFormat) error {
	flags, common := newFlagSet("get")
	if err := flags.Parse(args); err != nil {
		*failureFormat = parsedFormat(common.format)
		return invalidArguments(err.Error(), true)
	}
	*failureFormat = parsedFormat(common.format)
	format, err := validateCommon(common, flags.NArg(), 1)
	if err != nil {
		return err
	}
	if err := search.ValidateID(flags.Arg(0)); err != nil {
		return invalidArguments(err.Error(), true)
	}
	root, err := resolveProject(common.project.value)
	if err != nil {
		return err
	}
	path, err := resolveIndexPath(root, common.index.value)
	if err != nil {
		return classifyError(err)
	}
	value, err := indexpkg.Load(path, root)
	if err != nil {
		return retrievalIndexFailure(common.project.value, path, err)
	}
	response, err := search.Get(value, common.project.value, flags.Arg(0))
	if err != nil {
		return classifyError(err)
	}
	if format == formatJSON {
		return writeJSON(stdout, response)
	}
	if _, err := fmt.Fprintf(stdout, "Source: %s\nCitations: %s\nIndexed: %s\n\n%s",
		response.Card.Source, strings.Join(response.Card.Paths, ", "), response.IndexedAt, response.Card.Content); err != nil {
		return classifyError(fmt.Errorf("write get output: %w", err))
	}
	if !strings.HasSuffix(response.Card.Content, "\n") {
		if _, err := fmt.Fprintln(stdout); err != nil {
			return classifyError(fmt.Errorf("write get output: %w", err))
		}
	}
	return nil
}

func runProject(args []string, stdout io.Writer, failureFormat *outputFormat) error {
	if len(args) == 0 {
		return invalidArguments("a project subcommand is required", true)
	}
	switch args[0] {
	case "init":
		return runProjectInit(args[1:], stdout, failureFormat)
	case "remove":
		return runProjectRemove(args[1:], stdout, failureFormat)
	case "list":
		return runProjectList(args[1:], stdout, failureFormat)
	case "healthcheck":
		return runProjectHealthcheck(args[1:], stdout, failureFormat)
	default:
		return invalidArguments(fmt.Sprintf("unknown project subcommand %q", args[0]), true)
	}
}

func runProjectInit(args []string, stdout io.Writer, failureFormat *outputFormat) error {
	args = reorderArgs(args, map[string]bool{"--dir": true, "--format": true})
	flags := flag.NewFlagSet("project init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", "", "project directory (defaults to the working directory)")
	format := flags.String("format", string(formatText), "output format")
	if err := flags.Parse(args); err != nil {
		*failureFormat = parsedFormat(*format)
		return invalidArguments(err.Error(), true)
	}
	*failureFormat = parsedFormat(*format)
	if flags.NArg() != 1 {
		return invalidArguments(fmt.Sprintf("expected 1 positional argument, got %d", flags.NArg()), true)
	}
	parsed, err := parseFormat(*format)
	if err != nil {
		return err
	}
	target := *dir
	if target == "" {
		working, err := os.Getwd()
		if err != nil {
			return classifyError(fmt.Errorf("resolve working directory: %w", err))
		}
		target = working
	}
	result, err := project.Init(flags.Arg(0), target)
	if err != nil {
		return classifyError(err)
	}
	scaffolded, err := scaffold.Ensure(result.Root, result.ID)
	if err != nil {
		return &commandError{
			code:    "project_scaffold_failed",
			message: fmt.Sprintf("project %q is registered at %s, but the knowledge-base skeleton was not completed: %v", result.ID, result.Root, err),
			cause:   err,
		}
	}
	if parsed == formatJSON {
		return writeJSON(stdout, projectMutationOutput{
			Project:      result.ID,
			Root:         result.Root,
			PreviousRoot: result.PreviousRoot,
			Scaffolded:   scaffolded,
			Rebound:      result.Rebound,
			Created:      result.Created,
		})
	}
	switch {
	case result.Created:
		if _, err := fmt.Fprintf(stdout, "Registered %s -> %s\n", result.ID, result.Root); err != nil {
			return classifyError(fmt.Errorf("write project output: %w", err))
		}
	case result.Rebound:
		if _, err := fmt.Fprintf(stdout, "Rebound %s: %s -> %s\n", result.ID, result.PreviousRoot, result.Root); err != nil {
			return classifyError(fmt.Errorf("write project output: %w", err))
		}
	default:
		if _, err := fmt.Fprintf(stdout, "Refreshed %s -> %s\n", result.ID, result.Root); err != nil {
			return classifyError(fmt.Errorf("write project output: %w", err))
		}
	}
	if len(scaffolded) > 0 {
		if _, err := fmt.Fprintf(stdout, "Scaffolded %d knowledge-base files:\n", len(scaffolded)); err != nil {
			return classifyError(fmt.Errorf("write project output: %w", err))
		}
		for _, rel := range scaffolded {
			if _, err := fmt.Fprintf(stdout, "  %s\n", rel); err != nil {
				return classifyError(fmt.Errorf("write project output: %w", err))
			}
		}
	}
	return nil
}

func runProjectRemove(args []string, stdout io.Writer, failureFormat *outputFormat) error {
	args = reorderArgs(args, map[string]bool{"--format": true})
	flags := flag.NewFlagSet("project remove", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", string(formatText), "output format")
	if err := flags.Parse(args); err != nil {
		*failureFormat = parsedFormat(*format)
		return invalidArguments(err.Error(), true)
	}
	*failureFormat = parsedFormat(*format)
	if flags.NArg() != 1 {
		return invalidArguments(fmt.Sprintf("expected 1 positional argument, got %d", flags.NArg()), true)
	}
	parsed, err := parseFormat(*format)
	if err != nil {
		return err
	}
	result, err := project.Remove(flags.Arg(0))
	if err != nil {
		return classifyError(err)
	}
	if parsed == formatJSON {
		return writeJSON(stdout, projectMutationOutput{Project: result.ID, Root: result.Root})
	}
	if _, err := fmt.Fprintf(stdout, "Removed %s (%s)\n", result.ID, result.Root); err != nil {
		return classifyError(fmt.Errorf("write project output: %w", err))
	}
	return nil
}

func runProjectList(args []string, stdout io.Writer, failureFormat *outputFormat) error {
	args = reorderArgs(args, map[string]bool{"--format": true})
	flags := flag.NewFlagSet("project list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", string(formatText), "output format")
	if err := flags.Parse(args); err != nil {
		*failureFormat = parsedFormat(*format)
		return invalidArguments(err.Error(), true)
	}
	*failureFormat = parsedFormat(*format)
	if flags.NArg() != 0 {
		return invalidArguments(fmt.Sprintf("expected 0 positional argument, got %d", flags.NArg()), true)
	}
	parsed, err := parseFormat(*format)
	if err != nil {
		return err
	}
	registry, err := project.Load()
	if err != nil {
		return classifyError(err)
	}
	statuses, err := project.Statuses(registry)
	if err != nil {
		return classifyError(err)
	}
	if parsed == formatJSON {
		return writeJSON(stdout, projectsOutput{Projects: statuses})
	}
	if len(statuses) == 0 {
		if _, err := fmt.Fprintln(stdout, "No projects registered."); err != nil {
			return classifyError(fmt.Errorf("write project output: %w", err))
		}
		return nil
	}
	for _, status := range statuses {
		line := status.ID + " " + status.Root + " " + status.IndexPath
		if status.RootMissing {
			line += " [root missing]"
		}
		if status.IndexMissing {
			line += " [index missing]"
		}
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return classifyError(fmt.Errorf("write project output: %w", err))
		}
	}
	return nil
}

func parsedFormat(value string) outputFormat {
	if value == string(formatJSON) {
		return formatJSON
	}
	return formatText
}

// reorderArgs moves positional arguments after flags so the documented
// `project init <id> [--dir <path>]` order parses alongside flag-first order.
func reorderArgs(args []string, valueFlags map[string]bool) []string {
	flagArgs := make([]string, 0, len(args))
	positionals := make([]string, 0, 2)
	for index := 0; index < len(args); index++ {
		token := args[index]
		if strings.HasPrefix(token, "-") {
			flagArgs = append(flagArgs, token)
			name := strings.SplitN(token, "=", 2)[0]
			if !strings.Contains(token, "=") && valueFlags[name] && index+1 < len(args) {
				index++
				flagArgs = append(flagArgs, args[index])
			}
			continue
		}
		positionals = append(positionals, token)
	}
	return append(flagArgs, positionals...)
}

func runProjectHealthcheck(args []string, stdout io.Writer, failureFormat *outputFormat) error {
	args = reorderArgs(args, map[string]bool{"--format": true})
	flags := flag.NewFlagSet("project healthcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	verify := flags.Bool("verify", false, "verify each project's index content")
	purge := flags.Bool("purge", false, "delete dangling index directories")
	format := flags.String("format", string(formatText), "output format")
	if err := flags.Parse(args); err != nil {
		*failureFormat = parsedFormat(*format)
		return invalidArguments(err.Error(), true)
	}
	*failureFormat = parsedFormat(*format)
	if flags.NArg() != 0 {
		return invalidArguments(fmt.Sprintf("expected 0 positional argument, got %d", flags.NArg()), true)
	}
	parsed, err := parseFormat(*format)
	if err != nil {
		return err
	}

	findings := []project.Finding{}
	purged := []string{}
	registry, err := project.Load()
	if err != nil {
		kind := "project_registry_invalid"
		if errors.Is(err, project.ErrRegistryMissing) {
			kind = "project_registry_missing"
		}
		path := ""
		if registryPath, pathErr := project.RegistryPath(); pathErr == nil {
			path = registryPath
		}
		findings = append(findings, project.Finding{Kind: kind, Path: path, Detail: err.Error()})
	} else if *purge {
		var retained []project.Finding
		var purgeErr error
		purged, retained, purgeErr = project.PurgeDangling(registry)
		if purgeErr != nil {
			return classifyError(purgeErr)
		}
		findings, err = project.Healthcheck(registry, *verify)
		if err != nil {
			return classifyError(err)
		}
		findings = mergeFindings(findings, retained)
	} else {
		findings, err = project.Healthcheck(registry, *verify)
		if err != nil {
			return classifyError(err)
		}
	}
	if findings == nil {
		findings = []project.Finding{}
	}
	if purged == nil {
		purged = []string{}
	}

	if parsed == formatJSON {
		if err := writeJSON(stdout, healthcheckOutput{Findings: findings, Purged: purged}); err != nil {
			return err
		}
	} else {
		for _, finding := range findings {
			parts := []string{finding.Kind}
			if finding.Project != "" {
				parts = append(parts, finding.Project)
			}
			if finding.Path != "" {
				parts = append(parts, finding.Path)
			}
			if finding.Detail != "" {
				parts = append(parts, finding.Detail)
			}
			if _, err := fmt.Fprintln(stdout, strings.Join(parts, " ")); err != nil {
				return classifyError(fmt.Errorf("write healthcheck output: %w", err))
			}
		}
		for _, path := range purged {
			if _, err := fmt.Fprintf(stdout, "Purged: %s\n", path); err != nil {
				return classifyError(fmt.Errorf("write healthcheck output: %w", err))
			}
		}
		if len(findings) == 0 && len(purged) == 0 {
			if _, err := fmt.Fprintln(stdout, "No problems found."); err != nil {
				return classifyError(fmt.Errorf("write healthcheck output: %w", err))
			}
		}
	}
	if len(findings) > 0 {
		return &commandError{
			code:    "healthcheck_findings",
			message: fmt.Sprintf("healthcheck reported %d finding(s)", len(findings)),
			cause:   errors.New("healthcheck findings"),
		}
	}
	return nil
}

func newFlagSet(name string) (*flag.FlagSet, *commonFlags) {
	common := &commonFlags{}
	common.project.name = "--project"
	common.index.name = "--index"
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Var(&common.project, "project", "registered project ID")
	flags.Var(&common.index, "index", "index file override")
	flags.StringVar(&common.format, "format", string(formatText), "output format")
	return flags, common
}

func validateCommon(common *commonFlags, positional, expected int) (outputFormat, error) {
	if positional != expected {
		return "", invalidArguments(fmt.Sprintf("expected %d positional argument, got %d", expected, positional), true)
	}
	format, err := parseFormat(common.format)
	if err != nil {
		return "", err
	}
	if !common.project.set {
		return "", projectRequired("--project is required")
	}
	return format, nil
}

func parseFormat(value string) (outputFormat, error) {
	format := outputFormat(value)
	if format != formatText && format != formatJSON {
		return "", invalidArguments(fmt.Sprintf("unsupported format %q", value), true)
	}
	return format, nil
}

// resolveProject maps a selected --project value to its registered canonical
// root: registry load, ID syntax, registry lookup, then root liveness.
func resolveProject(id string) (string, error) {
	registry, err := project.Load()
	if err != nil {
		return "", classifyError(err)
	}
	if !projectIDPattern.MatchString(id) {
		return "", projectIDInvalid(id)
	}
	entry, registered := registry.Projects[id]
	if !registered {
		return "", projectUnknown(id, registry)
	}
	if info, err := os.Stat(entry.Root); err != nil || !info.IsDir() {
		return "", &commandError{
			code: "project_root_missing",
			message: fmt.Sprintf("project %q has no live root at %s; repair with: wfm-rag project init %s --dir <new path>",
				id, entry.Root, id),
			cause: errRootMissing,
		}
	}
	return entry.Root, nil
}

func resolveIndexPath(root, configured string) (string, error) {
	if configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolve index path %q: %w", configured, err)
		}
		return filepath.Clean(absolute), nil
	}
	return indexpkg.DefaultPathFromCanonical(root)
}

func sortedRegistryIDs(registry project.Registry) []string {
	ids := make([]string, 0, len(registry.Projects))
	for id := range registry.Projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// mergeFindings unions healthcheck findings with retained purge findings,
// preferring the retained entry (which carries the not-purged reason) and
// restoring the deterministic (kind, project, path) order.
func mergeFindings(current, retained []project.Finding) []project.Finding {
	type findingKey struct {
		kind    string
		project string
		path    string
	}
	merged := make([]project.Finding, 0, len(current)+len(retained))
	seen := make(map[findingKey]bool, len(current)+len(retained))
	for _, finding := range append(append([]project.Finding{}, retained...), current...) {
		key := findingKey{finding.Kind, finding.Project, finding.Path}
		if seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, finding)
	}
	sort.Slice(merged, func(i, j int) bool {
		left, right := merged[i], merged[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Project != right.Project {
			return left.Project < right.Project
		}
		return left.Path < right.Path
	})
	return merged
}

func projectRequired(message string) *commandError {
	return &commandError{code: "project_required", message: message, usage: true}
}

func projectIDInvalid(id string) *commandError {
	return &commandError{
		code:    "project_id_invalid",
		message: fmt.Sprintf("project ID %q is invalid; IDs must match %s", id, projectIDPattern.String()),
		cause:   project.ErrIDInvalid,
		usage:   true,
	}
}

func projectUnknown(id string, registry project.Registry) *commandError {
	ids := sortedRegistryIDs(registry)
	message := fmt.Sprintf("project %q is not registered; configured projects: %s", id, strings.Join(ids, ", "))
	if len(ids) == 0 {
		message = fmt.Sprintf("project %q is not registered; no projects are registered", id)
	}
	return &commandError{code: "project_unknown", message: message, cause: project.ErrUnknownID, usage: true}
}

// retrievalIndexFailure collapses every retrieval-path index load failure into
// the public index_missing code naming the project and its repair command.
func retrievalIndexFailure(id, path string, cause error) *commandError {
	return &commandError{
		code: "index_missing",
		message: fmt.Sprintf("project %q has no valid index at %s: %v. Repair: wfm-rag index --project %s",
			id, path, cause, id),
		cause: cause,
	}
}

func invalidArguments(message string, includeUsage bool) *commandError {
	return &commandError{code: "invalid_arguments", message: message, cause: search.ErrInvalidArguments, usage: includeUsage}
}

func classifyError(err error) *commandError {
	if err == nil {
		return nil
	}
	var existing *commandError
	if errors.As(err, &existing) {
		return existing
	}
	classified := &commandError{code: "operation_failed", message: err.Error(), cause: err}
	switch {
	case errors.Is(err, search.ErrInvalidArguments):
		classified.code = "invalid_arguments"
	case errors.Is(err, project.ErrIDInvalid):
		classified.code = "project_id_invalid"
	case errors.Is(err, project.ErrUnknownID):
		classified.code = "project_unknown"
	case errors.Is(err, project.ErrLimitExceeded):
		classified.code = "project_limit_exceeded"
	case errors.Is(err, project.ErrRegistryMissing):
		classified.code = "project_registry_missing"
	case errors.Is(err, project.ErrRegistryInvalid):
		classified.code = "project_registry_invalid"
	case errors.Is(err, project.ErrLocked):
		classified.code = "project_locked"
	case errors.Is(err, indexpkg.ErrInvalid):
		classified.code = "invalid_index"
	case errors.Is(err, cards.ErrDuplicateID):
		classified.code = "duplicate_card_id"
	case errors.Is(err, cards.ErrMalformedCard):
		classified.code = "malformed_card_corpus"
	case errors.Is(err, search.ErrCardNotFound):
		classified.code = "card_not_found"
	}
	return classified
}

func writeFailure(stderr io.Writer, format outputFormat, err error) int {
	classified := classifyError(err)
	if format == formatJSON {
		output := errorOutput{}
		output.Error.Code = classified.code
		output.Error.Message = classified.message
		if encodeErr := json.NewEncoder(stderr).Encode(output); encodeErr != nil {
			fmt.Fprintf(stderr, "error: %s\n", classified.message)
		}
	} else {
		fmt.Fprintf(stderr, "error: %s\n", classified.message)
		if classified.usage {
			fmt.Fprintln(stderr, usage)
		}
	}
	if exitCodeFor(classified.code) == 2 {
		return 2
	}
	return 1
}

func exitCodeFor(code string) int {
	switch code {
	case "invalid_arguments", "project_required", "project_unknown", "project_id_invalid", "project_limit_exceeded":
		return 2
	}
	return 1
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return classifyError(fmt.Errorf("write JSON output: %w", err))
	}
	return nil
}
