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
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wfm-rag/internal/cards"
	indexpkg "wfm-rag/internal/index"
	"wfm-rag/internal/mcpserver"
	"wfm-rag/internal/search"
)

const usage = `usage:
  wfm-rag index --root <repo> [--index <file>] [--format text|json]
  wfm-rag search --root <repo> [--index <file>] [--path <path>] [--package <pkg>] [--type <type>] [--limit 5] [--format text|json] <query>
  wfm-rag get --root <repo> [--index <file>] [--format text|json] <card-id>
  wfm-rag mcp --root <repo> [--index <file>]`

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

type commonFlags struct {
	root   string
	index  string
	format string
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
		err = runMCP(args[1:])
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

func runMCP(args []string) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var root, configuredIndex string
	flags.StringVar(&root, "root", "", "repository root")
	flags.StringVar(&configuredIndex, "index", "", "index file")
	if err := flags.Parse(args); err != nil {
		return invalidArguments(err.Error(), true)
	}
	if strings.TrimSpace(root) == "" {
		return invalidArguments("--root is required", true)
	}
	if flags.NArg() != 0 {
		return invalidArguments(fmt.Sprintf("expected 0 positional argument, got %d", flags.NArg()), true)
	}
	indexPath, err := resolveIndexPath(root, configuredIndex)
	if err != nil {
		return classifyError(err)
	}
	return mcpserver.Run(context.Background(), root, indexPath, &mcp.StdioTransport{})
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
	path, err := resolveIndexPath(common.root, common.index)
	if err != nil {
		return classifyError(err)
	}
	result, err := indexpkg.Rebuild(common.root, path)
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
	path, err := resolveIndexPath(common.root, common.index)
	if err != nil {
		return classifyError(err)
	}
	loaded, err := indexpkg.Load(path, common.root)
	if err != nil {
		return classifyError(err)
	}
	response, err := search.Search(loaded, query)
	if err != nil {
		return classifyError(err)
	}
	if format == formatJSON {
		return writeJSON(stdout, response)
	}
	if len(response.Results) == 0 {
		_, err := fmt.Fprintf(stdout, "Indexed: %s\nNo results.\n", response.IndexedAt)
		if err != nil {
			return classifyError(fmt.Errorf("write search output: %w", err))
		}
		return nil
	}
	if _, err := fmt.Fprintf(stdout, "Indexed: %s\n", response.IndexedAt); err != nil {
		return classifyError(fmt.Errorf("write search output: %w", err))
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
	path, err := resolveIndexPath(common.root, common.index)
	if err != nil {
		return classifyError(err)
	}
	loaded, err := indexpkg.Load(path, common.root)
	if err != nil {
		return classifyError(err)
	}
	response, err := search.Get(loaded, flags.Arg(0))
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

func newFlagSet(name string) (*flag.FlagSet, *commonFlags) {
	common := &commonFlags{}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&common.root, "root", "", "repository root")
	flags.StringVar(&common.index, "index", "", "index file")
	flags.StringVar(&common.format, "format", string(formatText), "output format")
	return flags, common
}

func validateCommon(common *commonFlags, positional, expected int) (outputFormat, error) {
	if strings.TrimSpace(common.root) == "" {
		return "", invalidArguments("--root is required", true)
	}
	if positional != expected {
		return "", invalidArguments(fmt.Sprintf("expected %d positional argument, got %d", expected, positional), true)
	}
	format := outputFormat(common.format)
	if format != formatText && format != formatJSON {
		return "", invalidArguments(fmt.Sprintf("unsupported format %q", common.format), true)
	}
	return format, nil
}

func resolveIndexPath(root, configured string) (string, error) {
	if configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolve index path %q: %w", configured, err)
		}
		return filepath.Clean(absolute), nil
	}
	return indexpkg.DefaultPath(root)
}

func parsedFormat(value string) outputFormat {
	if value == string(formatJSON) {
		return formatJSON
	}
	return formatText
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
	case errors.Is(err, indexpkg.ErrMissing):
		classified.code = "index_missing"
	case errors.Is(err, indexpkg.ErrUnsupportedVersion):
		classified.code = "unsupported_index"
	case errors.Is(err, indexpkg.ErrRootMismatch):
		classified.code = "index_root_mismatch"
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
	if classified.code == "invalid_arguments" {
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
