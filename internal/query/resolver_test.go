package query

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const SEED int64 = 250131

var alphanum = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func randString(rng *rand.Rand, n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = alphanum[rng.Intn(len(alphanum))]
	}
	return string(b)
}

func randTerms(rng *rand.Rand, a0 int, a1 int, b0 int, b1 int) []string {
	n := a0 + rand.Intn(a1)
	terms := make([]string, n)
	for i := range terms {
		length := b0 + rand.Intn(b1)
		terms[i] = randString(rng, length)
	}
	return terms
}

func TestFieldResolverDistribution(t *testing.T) {
	rng := rand.New(rand.NewSource(SEED))
	terms := randTerms(rng, 1024, 512, 8, 5)
	fr := newFieldResolver(rng, terms, "")
	count := make(map[string]int)
	itrs := 10000

	for i := range itrs {
		term := fr.next()
		if !slices.Contains(terms, term) {
			t.Fatalf("terms does not contain value '%v'\n", term)
		}
		count[term]++

		mx := -1
		mn := itrs + 1
		for _, v := range count {
			mx = max(mx, v)
			mn = min(mn, v)
		}

		mxWant := (i / len(terms)) + 1
		if mx > mxWant {
			t.Fatalf("expected maximum value %v, got %v\n", mxWant, mx)
		}
		mnWant := (i / len(terms))
		if mn < mnWant {
			t.Fatalf("expected minumum value %v, got %v", mnWant, mn)
		}
	}
}

type template1Query struct {
	Letter letter `json:"letter"`
}

type letter struct {
	Latin string `json:"latin"`
	Greek string `json:"greek"`
}

func keys(m map[any]struct{}) []any {
	keys := make([]any, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestResolveTemplate1(t *testing.T) {
	require := require.New(t)

	rng := rand.New(rand.NewSource(SEED))

	data, err := os.ReadFile("testdata/template1.json")
	require.Nil(err, "failed to read file %v", err)

	var tmpl Template
	err = json.Unmarshal(data, &tmpl)
	require.Nil(err, "error unmarshal template")

	latinTerms, err := tmpl.Bindings["Latin"].Terms()
	require.Nil(err, "error loading Latin terms")
	greekTerms, err := tmpl.Bindings["Greek"].Terms()
	require.Nil(err, "error loading Greek terms")

	n := max(len(latinTerms), len(greekTerms))
	extra := 8 + rng.Intn(16)
	itrs := n*2 + extra

	resv, err := NewResolver(tmpl, rng, false)
	require.Nil(err, "error creating resolver")

	latinSet1 := make(map[any]struct{})
	latinSet2 := make(map[any]struct{})
	greekSet1 := make(map[any]struct{})
	greekSet2 := make(map[any]struct{})

	for i := range itrs {
		q := resv.Query()

		if i < len(latinTerms) {
			latinSet1[q.Vars["Latin"]] = struct{}{}
		} else {
			latinSet2[q.Vars["Latin"]] = struct{}{}
		}
		if i < len(greekTerms) {
			greekSet1[q.Vars["Greek"]] = struct{}{}
		} else {
			greekSet2[q.Vars["Greek"]] = struct{}{}
		}

		raw, err := q.Json()
		require.Nil(err, "error render json for query: %v", q)
		var obj template1Query
		err = json.Unmarshal([]byte(raw), &obj)
		require.Nil(err, "error unmarshal raw json: %v", raw)

		require.Equal(q.Vars["Latin"], obj.Letter.Latin, "unexpected value from json object")
		require.Equal(q.Vars["Greek"], obj.Letter.Greek, "unexpected value from json object")
	}

	require.ElementsMatch(keys(latinSet1), keys(latinSet2), "missmatched values in latin sets")
	require.ElementsMatch(
		latinTerms,
		keys(latinSet1),
		"missmatched values in latin terms and latin set",
	)

	require.ElementsMatch(keys(greekSet1), keys(greekSet2), "missmatched values in greek sets")
	require.ElementsMatch(
		greekTerms,
		keys(greekSet1),
		"missmatched values in greek terms and greek set",
	)
}

func TestResolveTemplate2(t *testing.T) {
	require := require.New(t)

	rng := rand.New(rand.NewSource(SEED))

	data, err := os.ReadFile("testdata/template2.json")
	require.Nil(err, "failed to read file %v", err)

	var tmpl Template
	err = json.Unmarshal(data, &tmpl)
	require.Nil(err, "error unmarshal template")

	resv, err := NewResolver(tmpl, rng, false)
	require.Nil(err, "error creating resolver")

	type T struct {
		Phrase []string `json:"phrase"`
	}

	validPairs := [][]string{
		{"hans", "greta"},
		{"gin", "tonic"},
		{"day", "night"},
		{"hello", "world"},
		{"pulp", "fiction"},
	}
	validTriplets := [][]string{
		{"woman", "no", "cry"},
		{"twist", "and", "shout"},
		{"the", "hateful", "eight"},
	}
	visited := make(map[string]string)

	for range 5 {
		q := resv.Query()
		raw, err := q.Json()
		require.Nil(err, "failed to create json from query: %v", err)

		var x T
		err = json.Unmarshal(raw, &x)
		require.Nil(err, "failed to unmarshal json query: %v", err)

		pair := []string{x.Phrase[0], x.Phrase[2]}
		match := false
		for _, opt := range validPairs {
			if assert.ObjectsAreEqual(opt, pair) {
				match = true
				break
			}
		}
		require.True(match, "invalid pair value %v", pair)
		visited[pair[0]] = pair[1]

		triplet := []string{x.Phrase[3], x.Phrase[1], x.Phrase[4]}
		match = false
		for _, opt := range validTriplets {
			if assert.ObjectsAreEqual(opt, triplet) {
				match = true
				break
			}
		}
		require.True(match, "invalid triplet value %v", triplet)
	}

	for _, opt := range validPairs {
		require.Equal(opt[1], visited[opt[0]])
	}
}

// A binding with no values cannot render a query. The resolver used to build
// successfully and then panic on the first Query(), part-way into a run.
func TestNewResolverRejectsABindingWithNoTerms(t *testing.T) {
	t.Parallel()

	tmpl := Template{
		Name:     "empty-inline",
		Bindings: map[string]Binding{"Word": {Source: "inline", TermsInline: []string{}}},
		Template: json.RawMessage(`{"q":"{{.Word}}"}`),
	}

	_, err := NewResolver(tmpl, rand.New(rand.NewSource(SEED)), false)

	require.ErrorContains(t, err, `binding "Word" has no terms`)
}

func TestNewResolverRejectsAnEmptyTermsFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.txt")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	tmpl := Template{
		Name:     "empty-file",
		Bindings: map[string]Binding{"Word": {Source: "file", Path: path}},
		Template: json.RawMessage(`{"q":"{{.Word}}"}`),
	}

	_, err := NewResolver(tmpl, rand.New(rand.NewSource(SEED)), false)

	require.ErrorContains(t, err, "has no terms")
	require.ErrorContains(t, err, path, "the error must name the empty file")
}
