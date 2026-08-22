package search

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"wfm-rag/internal/cards"
	indexpkg "wfm-rag/internal/index"
)

const (
	DefaultLimit     = 5
	MaxLimit         = 20
	MaxQueryBytes    = 4_096
	MaxCardIDBytes   = cards.MaxIDBytes
	MaxFilterValues  = 20
	MaxFilterBytes   = 1_024
	MaxExcerptChars  = 800
	excerptLeadChars = 200
)

var (
	ErrInvalidArguments = errors.New("invalid search arguments")
	ErrCardNotFound     = errors.New("card not found")
)

// Query describes one deterministic search over a loaded index.
type Query struct {
	Text     string   `json:"query"`
	Paths    []string `json:"paths,omitempty"`
	Packages []string `json:"packages,omitempty"`
	Types    []string `json:"types,omitempty"`
	Limit    *int     `json:"limit,omitempty"`
}

// Response is the shared CLI and MCP search result.
type Response struct {
	Root      string   `json:"root"`
	IndexedAt string   `json:"indexed_at"`
	Query     string   `json:"query"`
	Results   []Result `json:"results"`
}

// Result is the highest-scoring chunk for one card.
type Result struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	Score     int      `json:"score"`
	Reasons   []string `json:"reasons"`
	Excerpt   string   `json:"excerpt"`
	Source    string   `json:"source"`
	Heading   string   `json:"heading"`
	Citations []string `json:"citations"`
	Ordinal   int      `json:"-"`
}

// GetResponse returns one complete indexed card.
type GetResponse struct {
	Root      string            `json:"root"`
	IndexedAt string            `json:"indexed_at"`
	Card      indexpkg.Document `json:"card"`
}

// Search scores, filters, and deduplicates chunks from one validated index.
func Search(value *indexpkg.Index, query Query) (Response, error) {
	if value == nil {
		return Response{}, fmt.Errorf("%w: index is required", ErrInvalidArguments)
	}
	normalizedQuery, terms, err := validateQuery(query)
	if err != nil {
		return Response{}, err
	}
	limit := DefaultLimit
	if query.Limit != nil {
		limit = *query.Limit
	}
	response := Response{
		Root:      value.Root,
		IndexedAt: value.GeneratedAt.UTC().Format(time.RFC3339Nano),
		Query:     query.Text,
		Results:   make([]Result, 0),
	}

	bestByCard := make(map[string]Result)
	for _, chunk := range value.Chunks {
		if !matchesFilters(chunk, query) {
			continue
		}
		result := score(chunk, query, normalizedQuery, terms)
		if result.Score == 0 {
			continue
		}
		previous, exists := bestByCard[result.ID]
		if !exists || result.Score > previous.Score || result.Score == previous.Score && result.Ordinal < previous.Ordinal {
			bestByCard[result.ID] = result
		}
	}
	for _, result := range bestByCard {
		response.Results = append(response.Results, result)
	}
	sort.Slice(response.Results, func(i, j int) bool {
		left, right := response.Results[i], response.Results[j]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		return left.Ordinal < right.Ordinal
	})
	if len(response.Results) > limit {
		response.Results = response.Results[:limit]
	}
	return response, nil
}

// ValidateQuery checks search input without executing retrieval.
func ValidateQuery(query Query) error {
	_, _, err := validateQuery(query)
	return err
}

// Get returns one complete card by exact, case-sensitive stable ID.
func Get(value *indexpkg.Index, id string) (GetResponse, error) {
	if value == nil {
		return GetResponse{}, fmt.Errorf("%w: index is required", ErrInvalidArguments)
	}
	if err := ValidateID(id); err != nil {
		return GetResponse{}, err
	}
	for _, document := range value.Documents {
		if document.ID == id {
			return GetResponse{
				Root:      value.Root,
				IndexedAt: value.GeneratedAt.UTC().Format(time.RFC3339Nano),
				Card:      document,
			}, nil
		}
	}
	return GetResponse{}, fmt.Errorf("%w: %q", ErrCardNotFound, id)
}

// ValidateID checks exact-card retrieval input without executing retrieval.
func ValidateID(id string) error {
	if !utf8.ValidString(id) || len(id) > MaxCardIDBytes || strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
		return fmt.Errorf("%w: card id must be 1-%d UTF-8 bytes without surrounding whitespace", ErrInvalidArguments, MaxCardIDBytes)
	}
	return nil
}

func validateQuery(query Query) (string, []string, error) {
	if !utf8.ValidString(query.Text) || len(query.Text) > MaxQueryBytes || strings.TrimSpace(query.Text) == "" {
		return "", nil, fmt.Errorf("%w: query must contain 1-%d UTF-8 bytes of non-whitespace text", ErrInvalidArguments, MaxQueryBytes)
	}
	if query.Limit != nil && (*query.Limit < 1 || *query.Limit > MaxLimit) {
		return "", nil, fmt.Errorf("%w: limit must be between 1 and %d when provided", ErrInvalidArguments, MaxLimit)
	}
	if err := validateFilters("path", query.Paths, false); err != nil {
		return "", nil, err
	}
	if err := validateFilters("package", query.Packages, false); err != nil {
		return "", nil, err
	}
	if err := validateFilters("type", query.Types, true); err != nil {
		return "", nil, err
	}
	terms := normalizeTerms(query.Text)
	return strings.Join(terms, " "), terms, nil
}

func validateFilters(kind string, values []string, cardTypes bool) error {
	if len(values) > MaxFilterValues {
		return fmt.Errorf("%w: %s filters exceed %d values", ErrInvalidArguments, kind, MaxFilterValues)
	}
	for _, value := range values {
		if !utf8.ValidString(value) || len(value) > MaxFilterBytes || strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return fmt.Errorf("%w: each %s filter must contain 1-%d UTF-8 bytes without surrounding whitespace", ErrInvalidArguments, kind, MaxFilterBytes)
		}
		if cardTypes && !cards.IsValidType(value) {
			return fmt.Errorf("%w: unsupported type filter %q", ErrInvalidArguments, value)
		}
	}
	return nil
}

func matchesFilters(chunk cards.Chunk, query Query) bool {
	if len(query.Types) > 0 && !slices.Contains(query.Types, chunk.CardType) {
		return false
	}
	if len(query.Packages) > 0 && len(matchingPackages(chunk.Packages, query.Packages)) == 0 {
		return false
	}
	return len(query.Paths) == 0 || len(matchingPaths(chunk.Paths, query.Paths)) > 0
}

func score(chunk cards.Chunk, query Query, normalizedQuery string, terms []string) Result {
	result := Result{
		ID:        chunk.CardID,
		Type:      chunk.CardType,
		Title:     chunk.Title,
		Reasons:   make([]string, 0),
		Source:    chunk.Source,
		Heading:   chunk.Heading,
		Citations: slices.Clone(chunk.Paths),
		Ordinal:   chunk.Ordinal,
	}
	normalizedID := strings.Join(normalizeTerms(chunk.CardID), " ")
	if normalizedID == normalizedQuery {
		result.Score += 100
		result.Reasons = append(result.Reasons, "id: "+chunk.CardID)
	}

	packageMatches := matchingPackages(chunk.Packages, query.Packages)
	if containsNormalizedValue(chunk.Packages, normalizedQuery) {
		packageMatches = append(packageMatches, valuesWithNormalizedValue(chunk.Packages, normalizedQuery)...)
	}
	packageMatches = sortedUnique(packageMatches)
	if len(packageMatches) > 0 {
		result.Score += 40
		for _, value := range packageMatches {
			result.Reasons = append(result.Reasons, "package: "+value)
		}
	}

	pathMatches := matchingPaths(chunk.Paths, query.Paths)
	if containsNormalizedValue(chunk.Paths, normalizedQuery) {
		pathMatches = append(pathMatches, valuesWithNormalizedValue(chunk.Paths, normalizedQuery)...)
	}
	pathMatches = sortedUnique(pathMatches)
	if len(pathMatches) > 0 {
		result.Score += 30
		for _, value := range pathMatches {
			result.Reasons = append(result.Reasons, "path: "+value)
		}
	}

	triggerMatches := metadataMatches(chunk.Triggers, terms)
	scopeMatches := metadataMatches(chunk.Scopes, terms)
	if len(triggerMatches)+len(scopeMatches) > 0 {
		result.Score += 20
		for _, value := range triggerMatches {
			result.Reasons = append(result.Reasons, "trigger: "+value)
		}
		for _, value := range scopeMatches {
			result.Reasons = append(result.Reasons, "scope: "+value)
		}
	}

	titleHeadingTerms := tokenSet(chunk.Title + "\n" + chunk.Heading)
	textTerms := tokenSet(chunk.Text)
	normalizedTitle := normalizeSequence(chunk.Title)
	normalizedHeading := normalizeSequence(chunk.Heading)
	normalizedText := normalizeSequence(chunk.Text)
	if normalizedQuery != "" && (containsPhrase(normalizedTitle, normalizedQuery) || containsPhrase(normalizedHeading, normalizedQuery) || containsPhrase(normalizedText, normalizedQuery)) {
		result.Score += 10
		result.Reasons = append(result.Reasons, "phrase: "+normalizedQuery)
	}
	var titleMatches, textMatches []string
	for _, term := range terms {
		if titleHeadingTerms[term] {
			result.Score += 3
			titleMatches = append(titleMatches, term)
		}
		if textTerms[term] {
			result.Score++
			textMatches = append(textMatches, term)
		}
	}
	if len(titleMatches) > 0 {
		result.Reasons = append(result.Reasons, "title/heading terms: "+strings.Join(titleMatches, ", "))
	}
	if len(textMatches) > 0 {
		result.Reasons = append(result.Reasons, "text terms: "+strings.Join(textMatches, ", "))
	}
	result.Excerpt = excerpt(chunk.Text, terms)
	return result
}

func normalizeTerms(value string) []string {
	seen := make(map[string]struct{})
	var terms []string
	for _, term := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func normalizeText(value string) string {
	return strings.Join(normalizeTerms(value), " ")
}

func normalizeSequence(value string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), " ")
}

func tokenSet(value string) map[string]bool {
	result := make(map[string]bool)
	for _, term := range normalizeTerms(value) {
		result[term] = true
	}
	return result
}

func containsPhrase(haystack, phrase string) bool {
	return strings.Contains(" "+haystack+" ", " "+phrase+" ")
}

func metadataMatches(values, queryTerms []string) []string {
	query := " " + strings.Join(queryTerms, " ") + " "
	var matches []string
	for _, value := range values {
		normalized := normalizeText(value)
		if normalized != "" && strings.Contains(query, " "+normalized+" ") {
			matches = append(matches, value)
		}
	}
	return sortedUnique(matches)
}

func containsNormalizedValue(values []string, normalizedQuery string) bool {
	return len(valuesWithNormalizedValue(values, normalizedQuery)) > 0
}

func valuesWithNormalizedValue(values []string, normalizedQuery string) []string {
	var matches []string
	for _, value := range values {
		if normalizeText(value) == normalizedQuery {
			matches = append(matches, value)
		}
	}
	return matches
}

func matchingPackages(citations, filters []string) []string {
	var matches []string
	for _, citation := range citations {
		for _, filter := range filters {
			if strings.EqualFold(citation, filter) {
				matches = append(matches, citation)
				break
			}
		}
	}
	return sortedUnique(matches)
}

func matchingPaths(citations, filters []string) []string {
	var matches []string
	for _, citation := range citations {
		cleanCitation := cleanPath(citation)
		for _, filter := range filters {
			cleanFilter := cleanPath(filter)
			if cleanFilter == cleanCitation || strings.HasPrefix(cleanFilter, cleanCitation+"/") {
				matches = append(matches, citation)
				break
			}
		}
	}
	return sortedUnique(matches)
}

func cleanPath(value string) string {
	return strings.TrimPrefix(path.Clean(strings.ReplaceAll(value, "\\", "/")), "./")
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	return slices.Compact(values)
}

func excerpt(text string, terms []string) string {
	runes := []rune(text)
	if len(runes) <= MaxExcerptChars {
		return text
	}
	matchRune := usefulMatchRune(runes, terms)
	if matchRune < 0 {
		matchRune = 0
	}
	start := max(0, matchRune-excerptLeadChars)
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	available := MaxExcerptChars - len([]rune(prefix))
	end := min(len(runes), start+available)
	if end < len(runes) {
		suffix = "..."
		end -= len([]rune(suffix))
	}
	return prefix + string(runes[start:end]) + suffix
}

type positionedToken struct {
	value string
	start int
}

func usefulMatchRune(runes []rune, terms []string) int {
	if len(terms) == 0 {
		return -1
	}
	tokens := positionedTokens(runes)
	if len(terms) <= len(tokens) {
		for start := 0; start <= len(tokens)-len(terms); start++ {
			matched := true
			for offset, term := range terms {
				if tokens[start+offset].value != term {
					matched = false
					break
				}
			}
			if matched {
				return tokens[start].start
			}
		}
	}
	termSet := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		termSet[term] = struct{}{}
	}
	for _, token := range tokens {
		if _, matched := termSet[token.value]; matched {
			return token.start
		}
	}
	return -1
}

func positionedTokens(runes []rune) []positionedToken {
	var tokens []positionedToken
	start := -1
	for offset, r := range runes {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if start < 0 {
				start = offset
			}
			continue
		}
		if start >= 0 {
			tokens = append(tokens, positionedToken{value: strings.ToLower(string(runes[start:offset])), start: start})
			start = -1
		}
	}
	if start >= 0 {
		tokens = append(tokens, positionedToken{value: strings.ToLower(string(runes[start:])), start: start})
	}
	return tokens
}
