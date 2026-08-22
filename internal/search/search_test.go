package search

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"wfm-rag/internal/cards"
	indexpkg "wfm-rag/internal/index"
)

func TestSearchScoreBoosts(t *testing.T) {
	tests := []struct {
		name        string
		query       Query
		chunk       cards.Chunk
		wantScore   int
		wantReasons []string
	}{
		{
			name:        "exact id",
			query:       Query{Text: "CARD-id"},
			chunk:       testChunk("card-id", "Unrelated", "Elsewhere", "body"),
			wantScore:   100,
			wantReasons: []string{"id: card-id"},
		},
		{
			name:  "exact package from query",
			query: Query{Text: "gopkg.in/ini.v1"},
			chunk: withChunk(testChunk("card", "Unrelated", "Elsewhere", "body"), func(chunk *cards.Chunk) {
				chunk.Packages = []string{"gopkg.in/ini.v1"}
			}),
			wantScore:   40,
			wantReasons: []string{"package: gopkg.in/ini.v1"},
		},
		{
			name:  "exact path hint",
			query: Query{Text: "unrelated", Paths: []string{"src/config/ini.go"}},
			chunk: withChunk(testChunk("card", "Title", "Heading", "body"), func(chunk *cards.Chunk) {
				chunk.Paths = []string{"src/config"}
			}),
			wantScore:   30,
			wantReasons: []string{"path: src/config"},
		},
		{
			name:  "trigger and scope share one category boost",
			query: Query{Text: "parser go"},
			chunk: withChunk(testChunk("card", "Title", "Heading", "body"), func(chunk *cards.Chunk) {
				chunk.Triggers = []string{"parser"}
				chunk.Scopes = []string{"go"}
			}),
			wantScore:   20,
			wantReasons: []string{"trigger: parser", "scope: go"},
		},
		{
			name:        "complete normalized phrase and text terms",
			query:       Query{Text: "Alpha, beta!"},
			chunk:       testChunk("card", "Title", "Heading", "before alpha-beta after"),
			wantScore:   12,
			wantReasons: []string{"phrase: alpha beta", "text terms: alpha, beta"},
		},
		{
			name:        "unique title and heading terms",
			query:       Query{Text: "alpha beta missing"},
			chunk:       testChunk("card", "Alpha", "Beta", "body"),
			wantScore:   6,
			wantReasons: []string{"title/heading terms: alpha, beta"},
		},
		{
			name:        "deduplicated text terms",
			query:       Query{Text: "ALPHA alpha!"},
			chunk:       testChunk("card", "Title", "Heading", "alpha"),
			wantScore:   11,
			wantReasons: []string{"phrase: alpha", "text terms: alpha"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := testIndex(test.chunk)
			response, err := Search(value, test.query)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if len(response.Results) != 1 {
				t.Fatalf("Results = %#v, want one", response.Results)
			}
			result := response.Results[0]
			if result.Score != test.wantScore {
				t.Errorf("Score = %d, want %d", result.Score, test.wantScore)
			}
			if !reflect.DeepEqual(result.Reasons, test.wantReasons) {
				t.Errorf("Reasons = %v, want %v", result.Reasons, test.wantReasons)
			}
		})
	}
}

func TestSearchAccumulatesFormulaInReasonOrder(t *testing.T) {
	chunk := testChunk("alpha-beta", "Alpha Beta", "Alpha Guidance", "alpha beta")
	chunk.Packages = []string{"alpha beta"}
	chunk.Paths = []string{"alpha beta"}
	chunk.Triggers = []string{"alpha"}
	chunk.Scopes = []string{"beta"}
	response, err := Search(testIndex(chunk), Query{Text: "alpha beta"})
	if err != nil {
		t.Fatal(err)
	}
	result := response.Results[0]
	if result.Score != 208 {
		t.Errorf("Score = %d, want 208", result.Score)
	}
	want := []string{
		"id: alpha-beta",
		"package: alpha beta",
		"path: alpha beta",
		"trigger: alpha",
		"scope: beta",
		"phrase: alpha beta",
		"title/heading terms: alpha, beta",
		"text terms: alpha, beta",
	}
	if !reflect.DeepEqual(result.Reasons, want) {
		t.Errorf("Reasons = %v, want %v", result.Reasons, want)
	}
}

func TestSearchDeduplicatesMetadataTerms(t *testing.T) {
	chunk := testChunk("card", "Title", "Heading", "body")
	chunk.Triggers = []string{"go-go"}
	response, err := Search(testIndex(chunk), Query{Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Score != 20 {
		t.Fatalf("Results = %#v, want trigger match", response.Results)
	}
}

func TestSearchPhrasePreservesDocumentTokenSequence(t *testing.T) {
	falsePositive := testChunk("false-positive", "Title", "Heading", "x alpha x beta")
	realPhrase := testChunk("real-phrase", "Title", "Heading", "alpha x beta alpha beta")
	response, err := Search(testIndex(falsePositive, realPhrase), Query{Text: "alpha beta"})
	if err != nil {
		t.Fatal(err)
	}
	results := make(map[string]Result)
	for _, result := range response.Results {
		results[result.ID] = result
	}
	if results["false-positive"].Score != 2 {
		t.Errorf("false-positive score = %d, want term-only score 2", results["false-positive"].Score)
	}
	if results["real-phrase"].Score != 12 {
		t.Errorf("real-phrase score = %d, want phrase plus term score 12", results["real-phrase"].Score)
	}
}

func TestSearchMetadataReasonsAreSortedAndCategoryBoostsAreSingle(t *testing.T) {
	chunk := testChunk("card", "Title", "Heading", "body")
	chunk.Packages = []string{"z/pkg", "a/pkg"}
	chunk.Paths = []string{"z/path", "a/path"}
	response, err := Search(testIndex(chunk), Query{
		Text:     "unrelated",
		Packages: []string{"z/pkg", "a/pkg"},
		Paths:    []string{"z/path", "a/path"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := response.Results[0]
	if result.Score != 70 {
		t.Errorf("Score = %d, want 70", result.Score)
	}
	want := []string{"package: a/pkg", "package: z/pkg", "path: a/path", "path: z/path"}
	if !reflect.DeepEqual(result.Reasons, want) {
		t.Errorf("Reasons = %v, want %v", result.Reasons, want)
	}
}

func TestSearchFiltersUseORWithinKindAndANDAcrossKinds(t *testing.T) {
	first := testChunk("first", "First", "Guidance", "needle")
	first.CardType = "pattern"
	first.Packages = []string{"pkg/first"}
	first.Paths = []string{"src/first"}
	second := testChunk("second", "Second", "Guidance", "needle")
	second.CardType = "dependency"
	second.Packages = []string{"pkg/second"}
	second.Paths = []string{"src/second.go"}
	third := testChunk("third", "Third", "Guidance", "needle")
	third.CardType = "pattern"
	third.Packages = []string{"pkg/third"}
	third.Paths = []string{"src/third.go"}

	response, err := Search(testIndex(first, second, third), Query{
		Text:     "needle",
		Packages: []string{"pkg/missing", "pkg/first"},
		Paths:    []string{"src/first/file.go", "src/third.go"},
		Types:    []string{"pattern"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultIDs(response.Results); !reflect.DeepEqual(got, []string{"first"}) {
		t.Errorf("result IDs = %v, want [first]", got)
	}
}

func TestSearchPathPrefixRequiresCitationSegmentBoundary(t *testing.T) {
	chunk := testChunk("card", "Card", "Heading", "needle")
	chunk.Paths = []string{"src/foo"}
	for _, filter := range []string{"src/foobar/file.go", "src"} {
		response, err := Search(testIndex(chunk), Query{Text: "needle", Paths: []string{filter}})
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Results) != 0 {
			t.Errorf("path filter %q returned %#v, want none", filter, response.Results)
		}
	}
}

func TestSearchDeduplicatesCardsAndOrdersDeterministically(t *testing.T) {
	a0 := testChunk("a", "A", "First", "needle")
	a0.Source = ".agents/knowledge/patterns/z.md"
	a0.Ordinal = 0
	a1 := a0
	a1.Heading = "Second"
	a1.Ordinal = 1
	a2 := a0
	a2.Heading = "Needle"
	a2.Text = "body"
	a2.Ordinal = 2
	b := testChunk("b", "B", "Heading", "needle")
	b.Source = ".agents/knowledge/patterns/a.md"
	c := testChunk("c", "Needle", "Heading", "body")
	c.Source = ".agents/knowledge/patterns/c.md"
	zero := testChunk("zero", "Zero", "Heading", "body")

	response, err := Search(testIndex(a1, zero, c, a0, b, a2), Query{Text: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultIDs(response.Results); !reflect.DeepEqual(got, []string{"c", "a", "b"}) {
		t.Errorf("result IDs = %v, want [c a b]", got)
	}
	if response.Results[1].Ordinal != 2 {
		t.Errorf("chosen ordinal for higher-scoring later chunk = %d, want 2", response.Results[1].Ordinal)
	}
}

func TestSearchLimitDefaultsAndApplies(t *testing.T) {
	var chunks []cards.Chunk
	for i, id := range []string{"a", "b", "c", "d", "e", "f"} {
		chunk := testChunk(id, strings.ToUpper(id), "Heading", "needle")
		chunk.Source = ".agents/knowledge/patterns/" + id + ".md"
		chunk.Ordinal = i
		chunks = append(chunks, chunk)
	}
	value := testIndex(chunks...)
	response, err := Search(value, Query{Text: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != DefaultLimit {
		t.Errorf("default results = %d, want %d", len(response.Results), DefaultLimit)
	}
	response, err = Search(value, Query{Text: "needle", Limit: intPointer(2)})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultIDs(response.Results); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("limited IDs = %v, want [a b]", got)
	}
}

func TestSearchExcerptHandlesCaseFoldByteLengthChanges(t *testing.T) {
	text := strings.Repeat("K", 900) + " needle " + strings.Repeat("x", 900)
	response, err := Search(testIndex(testChunk("card", "Title", "Heading", text)), Query{Text: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Results[0].Excerpt, "needle") {
		t.Errorf("excerpt omitted match after Unicode case fold: %q", response.Results[0].Excerpt)
	}
}

func TestSearchExcerptAnchorsOnTokenBoundary(t *testing.T) {
	text := strings.Repeat("algorithm ", 100) + strings.Repeat("x ", 500) + "go " + strings.Repeat("after ", 500)
	response, err := Search(testIndex(testChunk("card", "Title", "Heading", text)), Query{Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	excerpt := response.Results[0].Excerpt
	if !strings.Contains(excerpt, "go ") || strings.Contains(excerpt, "algorithm") {
		t.Errorf("excerpt anchored on substring instead of token: %q", excerpt)
	}
}

func TestSearchPunctuationOnlyQueryReturnsNoResults(t *testing.T) {
	response, err := Search(testIndex(testChunk("card", "Title", "Heading", "---")), Query{Text: "---"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(response.Results) != 0 {
		t.Errorf("Results = %#v, want none", response.Results)
	}
}

func TestSearchExcerptIsBoundedAroundUsefulMatch(t *testing.T) {
	text := strings.Repeat("界", 900) + " needle " + strings.Repeat("後", 900)
	chunk := testChunk("card", "Title", "Heading", text)
	response, err := Search(testIndex(chunk), Query{Text: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	excerpt := response.Results[0].Excerpt
	if got := utf8.RuneCountInString(excerpt); got > MaxExcerptChars {
		t.Errorf("excerpt characters = %d, want <= %d", got, MaxExcerptChars)
	}
	if !strings.Contains(excerpt, "needle") || !strings.HasPrefix(excerpt, "...") || !strings.HasSuffix(excerpt, "...") {
		t.Errorf("excerpt = %q, want bounded match context", excerpt)
	}

	short := "short needle"
	chunk.Text = short
	response, err = Search(testIndex(chunk), Query{Text: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Results[0].Excerpt != short {
		t.Errorf("short excerpt = %q, want unchanged", response.Results[0].Excerpt)
	}
}

func TestSearchValidatesInputs(t *testing.T) {
	value := testIndex(testChunk("card", "Title", "Heading", "body"))
	tests := []struct {
		name  string
		value *indexpkg.Index
		query Query
	}{
		{name: "nil index", value: nil, query: Query{Text: "query"}},
		{name: "empty query", value: value, query: Query{}},
		{name: "invalid utf8", value: value, query: Query{Text: string([]byte{0xff})}},
		{name: "long query", value: value, query: Query{Text: strings.Repeat("a", MaxQueryBytes+1)}},
		{name: "zero limit", value: value, query: Query{Text: "query", Limit: intPointer(0)}},
		{name: "negative limit", value: value, query: Query{Text: "query", Limit: intPointer(-1)}},
		{name: "large limit", value: value, query: Query{Text: "query", Limit: intPointer(MaxLimit + 1)}},
		{name: "many filters", value: value, query: Query{Text: "query", Paths: make([]string, MaxFilterValues+1)}},
		{name: "empty filter", value: value, query: Query{Text: "query", Packages: []string{""}}},
		{name: "spaced filter", value: value, query: Query{Text: "query", Paths: []string{" path"}}},
		{name: "long filter", value: value, query: Query{Text: "query", Packages: []string{strings.Repeat("a", MaxFilterBytes+1)}}},
		{name: "invalid type", value: value, query: Query{Text: "query", Types: []string{"unknown"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Search(test.value, test.query)
			if !errors.Is(err, ErrInvalidArguments) {
				t.Fatalf("Search() error = %v, want ErrInvalidArguments", err)
			}
		})
	}
}

func TestGetReturnsCompleteExactCard(t *testing.T) {
	chunk := testChunk("card-id", "Card", "Card", "# Card\n\nBody")
	value := testIndex(chunk)
	value.GeneratedAt = time.Date(2026, 8, 12, 12, 0, 0, 123, time.UTC)
	value.Documents = []indexpkg.Document{{
		Metadata: cards.Metadata{ID: "card-id", Type: "pattern", Status: "active", Paths: []string{"src/card.go"}},
		Title:    "Card",
		Source:   chunk.Source,
		Content:  "---\nid: card-id\n---\n# Card\n\nBody\n",
	}}

	response, err := Get(value, "card-id")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if response.Root != value.Root || response.IndexedAt != "2026-08-12T12:00:00.000000123Z" {
		t.Errorf("response identity = root %q, indexed %q", response.Root, response.IndexedAt)
	}
	if !reflect.DeepEqual(response.Card, value.Documents[0]) {
		t.Errorf("Card = %#v, want %#v", response.Card, value.Documents[0])
	}
	if _, err := Get(value, "CARD-ID"); !errors.Is(err, ErrCardNotFound) {
		t.Errorf("case-changing Get() error = %v, want ErrCardNotFound", err)
	}
	if _, err := Get(value, "missing"); !errors.Is(err, ErrCardNotFound) {
		t.Errorf("missing Get() error = %v, want ErrCardNotFound", err)
	}
}

func TestGetValidatesInputs(t *testing.T) {
	value := testIndex(testChunk("card", "Card", "Card", "body"))
	for _, test := range []struct {
		name  string
		value *indexpkg.Index
		id    string
	}{
		{name: "nil index", id: "card"},
		{name: "empty", value: value},
		{name: "whitespace", value: value, id: " card"},
		{name: "invalid utf8", value: value, id: string([]byte{0xff})},
		{name: "long", value: value, id: strings.Repeat("a", MaxCardIDBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Get(test.value, test.id)
			if !errors.Is(err, ErrInvalidArguments) {
				t.Fatalf("Get() error = %v, want ErrInvalidArguments", err)
			}
		})
	}
}

func TestResultJSONOmitsInternalOrdinal(t *testing.T) {
	encoded, err := json.Marshal(Result{ID: "card", Ordinal: 7})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "ordinal") {
		t.Errorf("JSON = %s, internal ordinal should be omitted", encoded)
	}
}

func testIndex(chunks ...cards.Chunk) *indexpkg.Index {
	return &indexpkg.Index{
		Version:     indexpkg.Version,
		Root:        "/repository",
		GeneratedAt: time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC),
		Documents:   []indexpkg.Document{},
		Chunks:      chunks,
	}
}

func testChunk(id, title, heading, text string) cards.Chunk {
	return cards.Chunk{
		CardID:    id,
		CardType:  "pattern",
		Title:     title,
		Source:    ".agents/knowledge/patterns/" + id + ".md",
		Heading:   heading,
		Text:      text,
		StartLine: 1,
		EndLine:   1,
	}
}

func withChunk(chunk cards.Chunk, change func(*cards.Chunk)) cards.Chunk {
	change(&chunk)
	return chunk
}

func resultIDs(results []Result) []string {
	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.ID
	}
	return ids
}

func intPointer(value int) *int {
	return &value
}
