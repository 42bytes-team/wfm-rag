package index

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"wfm-rag/internal/cards"
)

const (
	Version       = 1
	MaxIndexBytes = 512 << 20
)

var (
	ErrMissing            = errors.New("index missing")
	ErrInvalid            = errors.New("invalid index")
	ErrUnsupportedVersion = errors.New("unsupported index version")
	ErrRootMismatch       = errors.New("index belongs to another root")
)

// Index is the complete disposable retrieval state for one repository root.
type Index struct {
	Version     int           `json:"version"`
	Root        string        `json:"root"`
	GeneratedAt time.Time     `json:"generated_at"`
	Documents   []Document    `json:"documents"`
	Chunks      []cards.Chunk `json:"chunks"`
}

// Document stores one complete active card without duplicating its chunks.
type Document struct {
	cards.Metadata
	Title   string `json:"title"`
	Source  string `json:"source"`
	Content string `json:"content"`
}

// BuildResult includes the index and corpus diagnostics needed by the CLI.
type BuildResult struct {
	Index       *Index
	Path        string
	Skipped     int
	SourceBytes int64
	Warnings    []cards.Warning
}

// Build validates a complete corpus and constructs its in-memory index.
func Build(root string) (*BuildResult, error) {
	corpus, err := cards.Load(root)
	if err != nil {
		return nil, fmt.Errorf("build index: %w", err)
	}
	built := &Index{
		Version:     Version,
		Root:        corpus.Root,
		GeneratedAt: time.Now().UTC(),
		Documents:   make([]Document, 0, len(corpus.Cards)),
		Chunks:      make([]cards.Chunk, 0),
	}
	for _, card := range corpus.Cards {
		built.Documents = append(built.Documents, Document{
			Metadata: card.Metadata,
			Title:    card.Title,
			Source:   card.Source,
			Content:  card.Content,
		})
		built.Chunks = append(built.Chunks, card.Chunks...)
	}
	if err := validate(built); err != nil {
		return nil, fmt.Errorf("build index: %w", err)
	}
	return &BuildResult{
		Index:       built,
		Skipped:     corpus.Skipped,
		SourceBytes: corpus.SourceBytes,
		Warnings:    slices.Clone(corpus.Warnings),
	}, nil
}

// Rebuild constructs and atomically replaces the complete index. An empty path
// selects the default per-root location.
func Rebuild(root, path string) (*BuildResult, error) {
	result, err := Build(root)
	if err != nil {
		return nil, err
	}
	if path == "" {
		path, err = DefaultPath(result.Index.Root)
		if err != nil {
			return nil, err
		}
	}
	if err := Write(path, result.Index); err != nil {
		return nil, err
	}
	result.Path = path
	return result, nil
}

// DefaultPath returns the stable per-root index location.
func DefaultPath(root string) (string, error) {
	canonical, err := cards.CanonicalRoot(root)
	if err != nil {
		return "", fmt.Errorf("resolve default index path: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve default index path: %w", err)
	}
	digest := sha256.Sum256([]byte(canonical))
	return filepath.Join(home, ".local", "share", "wfm-rag", hex.EncodeToString(digest[:]), "index.json"), nil
}

// Write atomically replaces path with index after syncing a temporary sibling.
func Write(path string, value *Index) error {
	return write(path, value, MaxIndexBytes)
}

func write(path string, value *Index, maxBytes int64) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("write index: path is required")
	}
	if err := validate(value); err != nil {
		return fmt.Errorf("write index %q: %w", path, err)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create index directory %q: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, ".index-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary index beside %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			temporary.Close()
			os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary index permissions %q: %w", temporaryPath, err)
	}

	written := &limitedWriter{writer: temporary, remaining: maxBytes, limit: maxBytes}
	encoder := json.NewEncoder(written)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode index %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary index %q: %w", temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary index %q: %w", temporaryPath, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace index %q: %w", path, err)
	}
	keepTemporary = false
	return nil
}

// Load reads and validates an existing index for the expected repository root.
func Load(path, expectedRoot string) (*Index, error) {
	canonical, err := cards.CanonicalRoot(expectedRoot)
	if err != nil {
		return nil, fmt.Errorf("load index %q: %w", path, err)
	}
	data, err := readFile(path, MaxIndexBytes)
	if err != nil {
		return nil, err
	}
	var loaded Index
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&loaded); err != nil {
		return nil, fmt.Errorf("%w: decode index %q: %v", ErrInvalid, path, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("%w: decode index %q: %v", ErrInvalid, path, err)
	}
	if loaded.Version != Version {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedVersion, loaded.Version, Version)
	}
	if err := validate(&loaded); err != nil {
		return nil, fmt.Errorf("load index %q: %w", path, err)
	}
	if loaded.Root != canonical {
		return nil, fmt.Errorf("%w: index root %q does not match %q", ErrRootMismatch, loaded.Root, canonical)
	}
	return &loaded, nil
}

func readFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %q", ErrMissing, path)
		}
		return nil, fmt.Errorf("inspect index %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: index %q is not a regular file", ErrInvalid, path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: index %q exceeds %d bytes", ErrInvalid, path, maxBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open index %q: %w", path, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open index %q: %w", path, err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("%w: index %q changed while opening", ErrInvalid, path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read index %q: %w", path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: index %q exceeds %d bytes", ErrInvalid, path, maxBytes)
	}
	return data, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validate(value *Index) error {
	if value == nil {
		return fmt.Errorf("%w: nil value", ErrInvalid)
	}
	if value.Version != Version {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedVersion, value.Version, Version)
	}
	if value.Root == "" || !filepath.IsAbs(value.Root) || filepath.Clean(value.Root) != value.Root {
		return fmt.Errorf("%w: root must be a clean absolute path", ErrInvalid)
	}
	if value.GeneratedAt.IsZero() {
		return fmt.Errorf("%w: generated_at is required", ErrInvalid)
	}
	if value.Documents == nil || value.Chunks == nil {
		return fmt.Errorf("%w: documents and chunks are required", ErrInvalid)
	}
	documents := make(map[string]struct{}, len(value.Documents))
	documentByID := make(map[string]Document, len(value.Documents))
	sources := make(map[string]struct{}, len(value.Documents))
	for _, document := range value.Documents {
		if document.ID == "" || len(document.ID) > cards.MaxIDBytes || document.Title == "" || document.Source == "" || document.Content == "" {
			return fmt.Errorf("%w: document id, title, source, and content are required", ErrInvalid)
		}
		if !cards.IsValidType(document.Type) {
			return fmt.Errorf("%w: document %q has unsupported type %q", ErrInvalid, document.ID, document.Type)
		}
		if document.Status != "active" {
			return fmt.Errorf("%w: document %q is not active", ErrInvalid, document.ID)
		}
		if !cards.IsCardSource(document.Source) {
			return fmt.Errorf("%w: document %q has invalid source %q", ErrInvalid, document.ID, document.Source)
		}
		if _, exists := documents[document.ID]; exists {
			return fmt.Errorf("%w: duplicate document id %q", ErrInvalid, document.ID)
		}
		if _, exists := sources[document.Source]; exists {
			return fmt.Errorf("%w: duplicate document source %q", ErrInvalid, document.Source)
		}
		documents[document.ID] = struct{}{}
		documentByID[document.ID] = document
		sources[document.Source] = struct{}{}
	}
	ordinals := make(map[string]map[int]struct{}, len(documents))
	for _, chunk := range value.Chunks {
		document, exists := documentByID[chunk.CardID]
		if !exists {
			return fmt.Errorf("%w: chunk references unknown card %q", ErrInvalid, chunk.CardID)
		}
		if chunk.Ordinal < 0 || chunk.Source == "" || chunk.Title == "" || chunk.Heading == "" || chunk.Text == "" {
			return fmt.Errorf("%w: chunk for card %q is incomplete", ErrInvalid, chunk.CardID)
		}
		if chunk.StartLine < 1 || chunk.EndLine < chunk.StartLine {
			return fmt.Errorf("%w: chunk %d for card %q has invalid line range", ErrInvalid, chunk.Ordinal, chunk.CardID)
		}
		if chunk.CardType != document.Type || chunk.Title != document.Title || chunk.Source != document.Source ||
			!slices.Equal(chunk.Scopes, document.Scopes) || !slices.Equal(chunk.Triggers, document.Triggers) ||
			!slices.Equal(chunk.Packages, document.Packages) || !slices.Equal(chunk.Paths, document.Paths) {
			return fmt.Errorf("%w: chunk %d does not match document %q", ErrInvalid, chunk.Ordinal, chunk.CardID)
		}
		if ordinals[chunk.CardID] == nil {
			ordinals[chunk.CardID] = make(map[int]struct{})
		}
		if _, exists := ordinals[chunk.CardID][chunk.Ordinal]; exists {
			return fmt.Errorf("%w: duplicate chunk ordinal %d for card %q", ErrInvalid, chunk.Ordinal, chunk.CardID)
		}
		ordinals[chunk.CardID][chunk.Ordinal] = struct{}{}
	}
	for id := range documents {
		cardOrdinals := ordinals[id]
		if len(cardOrdinals) == 0 {
			return fmt.Errorf("%w: document %q has no chunks", ErrInvalid, id)
		}
		for ordinal := range len(cardOrdinals) {
			if _, exists := cardOrdinals[ordinal]; !exists {
				return fmt.Errorf("%w: document %q has non-contiguous chunk ordinals", ErrInvalid, id)
			}
		}
	}
	return nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining int64
	limit     int64
}

func (writer *limitedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > writer.remaining {
		return 0, fmt.Errorf("%w: encoded index exceeds %d bytes", ErrInvalid, writer.limit)
	}
	n, err := writer.writer.Write(data)
	writer.remaining -= int64(n)
	return n, err
}
