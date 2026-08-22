package cards

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

const (
	MaxFileBytes   int64 = 1 << 20
	MaxCorpusBytes int64 = 100 << 20
	MaxCards             = 10_000
	MaxChunkChars        = 8_000
	MaxIDBytes           = 256
)

var (
	ErrMalformedCard = errors.New("malformed card corpus")
	ErrDuplicateID   = errors.New("duplicate card id")

	validTypes  = []string{"anti-pattern", "decision", "dependency", "incident", "pattern", "reference"}
	validStatus = []string{"active", "draft", "retired"}
	categories  = map[string]struct{}{
		"anti-patterns": {},
		"dependencies":  {},
		"incidents":     {},
		"patterns":      {},
		"sources":       {},
	}
)

// IsValidType reports whether value belongs to the knowledge card schema.
func IsValidType(value string) bool {
	return slices.Contains(validTypes, value)
}

// IsValidStatus reports whether value belongs to the knowledge card schema.
func IsValidStatus(value string) bool {
	return slices.Contains(validStatus, value)
}

// IsCardSource reports whether source is a normalized categorized card path.
func IsCardSource(source string) bool {
	if source == "" || strings.Contains(source, "\\") || path.IsAbs(source) || path.Clean(source) != source {
		return false
	}
	parts := strings.Split(source, "/")
	if len(parts) < 4 || parts[0] != ".agents" || parts[1] != "knowledge" {
		return false
	}
	if _, ok := categories[parts[2]]; !ok {
		return false
	}
	return path.Ext(source) == ".md" && path.Base(source) != "README.md"
}

// Metadata is the structured frontmatter shared by a card and its chunks.
type Metadata struct {
	ID               string   `json:"id" yaml:"id"`
	Type             string   `json:"type" yaml:"type"`
	Status           string   `json:"status" yaml:"status"`
	Scopes           []string `json:"scopes" yaml:"scope"`
	Triggers         []string `json:"triggers" yaml:"triggers"`
	Packages         []string `json:"packages" yaml:"packages"`
	Paths            []string `json:"paths" yaml:"paths"`
	SourceConfidence string   `json:"source_confidence,omitempty" yaml:"source_confidence"`
	LastVerified     string   `json:"last_verified,omitempty" yaml:"last_verified"`
}

// Card is one validated active knowledge document.
type Card struct {
	Metadata
	Title   string  `json:"title"`
	Source  string  `json:"source"`
	Content string  `json:"content"`
	Chunks  []Chunk `json:"chunks"`
}

// Chunk is a bounded, searchable section of a card.
type Chunk struct {
	CardID    string   `json:"card_id"`
	CardType  string   `json:"card_type"`
	Title     string   `json:"title"`
	Scopes    []string `json:"scopes"`
	Triggers  []string `json:"triggers"`
	Packages  []string `json:"packages"`
	Paths     []string `json:"paths"`
	Source    string   `json:"source"`
	Heading   string   `json:"heading"`
	Ordinal   int      `json:"ordinal"`
	Text      string   `json:"text"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
}

// Warning describes a non-fatal corpus issue in deterministic discovery order.
type Warning struct {
	Source  string `json:"source"`
	CardID  string `json:"card_id,omitempty"`
	Message string `json:"message"`
}

// Corpus is the complete result of loading one repository's knowledge cards.
type Corpus struct {
	Root        string    `json:"root"`
	Cards       []Card    `json:"cards"`
	Skipped     int       `json:"skipped"`
	SourceBytes int64     `json:"source_bytes"`
	Warnings    []Warning `json:"warnings"`
}

type limits struct {
	fileBytes   int64
	corpusBytes int64
	cards       int
}

// CanonicalRoot resolves root to an absolute, symlink-free directory path.
func CanonicalRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("canonicalize root: root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("canonicalize root %q: %w", root, err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("canonicalize root %q: %w", abs, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("stat root %q: %w", canonical, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("canonicalize root %q: not a directory", canonical)
	}
	return filepath.Clean(canonical), nil
}

// Load discovers and validates all categorized Markdown cards beneath root.
func Load(root string) (*Corpus, error) {
	return load(root, limits{fileBytes: MaxFileBytes, corpusBytes: MaxCorpusBytes, cards: MaxCards})
}

func load(root string, bounds limits) (*Corpus, error) {
	canonical, err := CanonicalRoot(root)
	if err != nil {
		return nil, err
	}
	agentsRoot := filepath.Join(canonical, ".agents")
	if err := requireRealDirectory(agentsRoot); err != nil {
		return nil, fmt.Errorf("open agent directory %q: %w", agentsRoot, err)
	}
	knowledgeRoot := filepath.Join(agentsRoot, "knowledge")
	if err := requireRealDirectory(knowledgeRoot); err != nil {
		return nil, fmt.Errorf("open knowledge corpus %q: %w", knowledgeRoot, err)
	}

	corpus := &Corpus{Root: canonical}
	seenIDs := make(map[string]string)
	discovered := 0
	err = filepath.WalkDir(knowledgeRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk knowledge corpus at %q: %w", path, walkErr)
		}
		if path == knowledgeRoot {
			return nil
		}

		relKnowledge, err := filepath.Rel(knowledgeRoot, path)
		if err != nil {
			return fmt.Errorf("resolve corpus path %q: %w", path, err)
		}
		parts := strings.Split(filepath.ToSlash(relKnowledge), "/")
		_, allowed := categories[parts[0]]
		if !allowed {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(entry.Name()) != ".md" || entry.Name() == "README.md" {
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect card %q: %w", path, err)
		}
		if !entryInfo.Mode().IsRegular() {
			return nil
		}
		if entryInfo.Size() > bounds.fileBytes {
			return fmt.Errorf("read card %q: file size %d exceeds %d bytes", path, entryInfo.Size(), bounds.fileBytes)
		}
		discovered++
		if discovered > bounds.cards {
			return fmt.Errorf("load corpus: card count exceeds %d", bounds.cards)
		}
		data, err := readBoundedRegularFile(path, entryInfo, bounds.fileBytes)
		if err != nil {
			return err
		}
		corpus.SourceBytes += int64(len(data))
		if corpus.SourceBytes > bounds.corpusBytes {
			return fmt.Errorf("load corpus: source Markdown exceeds %d bytes", bounds.corpusBytes)
		}
		source, err := filepath.Rel(canonical, path)
		if err != nil {
			return fmt.Errorf("resolve card source %q: %w", path, err)
		}
		source = filepath.ToSlash(source)
		card, missing, err := parseCard(data, source)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrMalformedCard, err)
		}
		if previous, exists := seenIDs[card.ID]; exists {
			return fmt.Errorf("%w: parse card %q: duplicate id %q also used by %q", ErrDuplicateID, source, card.ID, previous)
		}
		seenIDs[card.ID] = source
		if len(missing) > 0 {
			corpus.Warnings = append(corpus.Warnings, Warning{
				Source:  source,
				CardID:  card.ID,
				Message: "optional metadata omitted: " + strings.Join(missing, ", "),
			})
		}
		if card.Status != "active" {
			corpus.Skipped++
			corpus.Warnings = append(corpus.Warnings, Warning{
				Source:  source,
				CardID:  card.ID,
				Message: "skipped card with status " + card.Status,
			})
			return nil
		}
		corpus.Cards = append(corpus.Cards, card)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return corpus, nil
}

func requireRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("not a regular directory")
	}
	return nil
}

func readBoundedRegularFile(path string, walkedInfo fs.FileInfo, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open card %q: %w", path, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open card %q: %w", path, err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(walkedInfo, openedInfo) {
		return nil, fmt.Errorf("read card %q: file changed during discovery", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read card %q: %w", path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("read card %q: file exceeds %d bytes", path, maxBytes)
	}
	return data, nil
}

func parseCard(data []byte, source string) (Card, []string, error) {
	frontmatter, body, bodyStartLine, err := splitFrontmatter(data)
	if err != nil {
		return Card{}, nil, fmt.Errorf("parse card %q: %w", source, err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(frontmatter, &node); err != nil {
		return Card{}, nil, fmt.Errorf("parse card %q frontmatter: %w", source, err)
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return Card{}, nil, fmt.Errorf("parse card %q frontmatter: expected a mapping", source)
	}
	var metadata Metadata
	if err := node.Decode(&metadata); err != nil {
		return Card{}, nil, fmt.Errorf("parse card %q frontmatter: %w", source, err)
	}
	metadata.ID = strings.TrimSpace(metadata.ID)
	metadata.Type = strings.TrimSpace(metadata.Type)
	metadata.Status = strings.TrimSpace(metadata.Status)
	if metadata.ID == "" || metadata.Type == "" || metadata.Status == "" {
		return Card{}, nil, fmt.Errorf("parse card %q frontmatter: id, type, and status are required", source)
	}
	if !utf8.ValidString(metadata.ID) || len(metadata.ID) > MaxIDBytes {
		return Card{}, nil, fmt.Errorf("parse card %q frontmatter: id must be valid UTF-8 and at most %d bytes", source, MaxIDBytes)
	}
	if !IsValidType(metadata.Type) {
		return Card{}, nil, fmt.Errorf("parse card %q frontmatter: unsupported type %q", source, metadata.Type)
	}
	if !IsValidStatus(metadata.Status) {
		return Card{}, nil, fmt.Errorf("parse card %q frontmatter: unsupported status %q", source, metadata.Status)
	}

	title, sections, err := markdownSections(body)
	if err != nil {
		return Card{}, nil, fmt.Errorf("parse card %q: %w", source, err)
	}
	card := Card{
		Metadata: metadata,
		Title:    title,
		Source:   source,
		Content:  string(data),
	}
	ordinal := 0
	for _, section := range sections {
		for _, span := range boundedSpans(body, section.start, section.end) {
			card.Chunks = append(card.Chunks, Chunk{
				CardID:    metadata.ID,
				CardType:  metadata.Type,
				Title:     title,
				Scopes:    slices.Clone(metadata.Scopes),
				Triggers:  slices.Clone(metadata.Triggers),
				Packages:  slices.Clone(metadata.Packages),
				Paths:     slices.Clone(metadata.Paths),
				Source:    source,
				Heading:   section.heading,
				Ordinal:   ordinal,
				Text:      string(body[span.start:span.end]),
				StartLine: bodyStartLine + bytes.Count(body[:span.start], []byte{'\n'}),
				EndLine:   bodyStartLine + bytes.Count(body[:span.end-1], []byte{'\n'}),
			})
			ordinal++
		}
	}
	return card, missingOptionalFields(node.Content[0]), nil
}

func splitFrontmatter(data []byte) ([]byte, []byte, int, error) {
	lineEnd := bytes.IndexByte(data, '\n')
	if lineEnd < 0 || string(bytes.TrimSpace(data[:lineEnd])) != "---" {
		return nil, nil, 0, errors.New("missing YAML frontmatter opening delimiter")
	}
	offset := lineEnd + 1
	line := 2
	for offset <= len(data) {
		next := bytes.IndexByte(data[offset:], '\n')
		end := len(data)
		bodyOffset := len(data)
		if next >= 0 {
			end = offset + next
			bodyOffset = end + 1
		}
		if string(bytes.TrimSpace(data[offset:end])) == "---" {
			return data[lineEnd+1 : offset], data[bodyOffset:], line + 1, nil
		}
		if next < 0 {
			break
		}
		offset = bodyOffset
		line++
	}
	return nil, nil, 0, errors.New("missing YAML frontmatter closing delimiter")
}

type section struct {
	heading    string
	start, end int
}

func markdownSections(body []byte) (string, []section, error) {
	document := goldmark.DefaultParser().Parse(text.NewReader(body))
	var title string
	var titleStart int
	var headings []section
	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		start := headingLineStart(body, heading.Lines().At(0).Start)
		if heading.Level == 1 && title == "" {
			title = strings.TrimSpace(string(heading.Text(body)))
			titleStart = start
			return ast.WalkContinue, nil
		}
		if heading.Level == 2 && title != "" && start > titleStart {
			headings = append(headings, section{
				heading: strings.TrimSpace(string(heading.Text(body))),
				start:   start,
			})
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("parse Markdown: %w", err)
	}
	if title == "" {
		return "", nil, errors.New("first level-one heading is required as the title")
	}
	sections := make([]section, 0, len(headings)+1)
	firstEnd := len(body)
	if len(headings) > 0 {
		firstEnd = headings[0].start
	}
	sections = append(sections, section{heading: title, start: titleStart, end: firstEnd})
	for i := range headings {
		headings[i].end = len(body)
		if i+1 < len(headings) {
			headings[i].end = headings[i+1].start
		}
		sections = append(sections, headings[i])
	}
	return title, sections, nil
}

func headingLineStart(body []byte, offset int) int {
	if previous := bytes.LastIndexByte(body[:offset], '\n'); previous >= 0 {
		return previous + 1
	}
	return 0
}

type span struct {
	start, end int
}

func boundedSpans(body []byte, start, end int) []span {
	start, end = trimSpan(body, start, end)
	if start == end {
		return nil
	}
	if utf8.RuneCount(body[start:end]) <= MaxChunkChars {
		return []span{{start: start, end: end}}
	}
	paragraphs := paragraphSpans(body, start, end)
	var result []span
	var current span
	for _, paragraph := range paragraphs {
		if utf8.RuneCount(body[paragraph.start:paragraph.end]) > MaxChunkChars {
			if current.end > current.start {
				result = append(result, current)
				current = span{}
			}
			result = append(result, hardSplitSpan(body, paragraph)...)
			continue
		}
		if current.end == 0 {
			current = paragraph
			continue
		}
		if utf8.RuneCount(body[current.start:paragraph.end]) <= MaxChunkChars {
			current.end = paragraph.end
			continue
		}
		result = append(result, current)
		current = paragraph
	}
	if current.end > current.start {
		result = append(result, current)
	}
	return result
}

func paragraphSpans(body []byte, start, end int) []span {
	var result []span
	paragraphStart := -1
	for offset := start; offset < end; {
		next := bytes.IndexByte(body[offset:end], '\n')
		lineEnd := end
		nextOffset := end
		if next >= 0 {
			lineEnd = offset + next
			nextOffset = lineEnd + 1
		}
		blank := len(bytes.TrimSpace(body[offset:lineEnd])) == 0
		if !blank && paragraphStart < 0 {
			paragraphStart = offset
		}
		if blank && paragraphStart >= 0 {
			paragraphEnd := offset
			for paragraphEnd > paragraphStart && (body[paragraphEnd-1] == '\n' || body[paragraphEnd-1] == '\r') {
				paragraphEnd--
			}
			result = append(result, span{start: paragraphStart, end: paragraphEnd})
			paragraphStart = -1
		}
		offset = nextOffset
	}
	if paragraphStart >= 0 {
		_, trimmedEnd := trimSpan(body, paragraphStart, end)
		result = append(result, span{start: paragraphStart, end: trimmedEnd})
	}
	return result
}

func hardSplitSpan(body []byte, value span) []span {
	var result []span
	partStart := value.start
	runes := 0
	for offset := value.start; offset < value.end; {
		_, size := utf8.DecodeRune(body[offset:value.end])
		if runes == MaxChunkChars {
			result = append(result, span{start: partStart, end: offset})
			partStart = offset
			runes = 0
		}
		offset += size
		runes++
	}
	if partStart < value.end {
		result = append(result, span{start: partStart, end: value.end})
	}
	return result
}

func trimSpan(body []byte, start, end int) (int, int) {
	for start < end && (body[start] == ' ' || body[start] == '\t' || body[start] == '\r' || body[start] == '\n') {
		start++
	}
	for end > start && (body[end-1] == ' ' || body[end-1] == '\t' || body[end-1] == '\r' || body[end-1] == '\n') {
		end--
	}
	return start, end
}

func missingOptionalFields(mapping *yaml.Node) []string {
	present := make(map[string]bool)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		present[mapping.Content[i].Value] = true
	}
	fields := []string{"scope", "triggers", "packages", "paths", "source_confidence", "last_verified"}
	return slices.DeleteFunc(fields, func(field string) bool { return present[field] })
}
