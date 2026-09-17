package query

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTemplate1 is a high-level integration test that loads and verifies a complete
// template definition from JSON. It checks the metadata, binding keys, and ensures
// that both file-based and inline bindings resolve to the correct terms.
// This test is designed to catch regressions in template structure or content.
func TestTemplate1(t *testing.T) {
	require := require.New(t)

	data, err := os.ReadFile("testdata/template1.json")
	require.Nil(err, "failed to read file %v", err)

	var tmpl Template
	err = json.Unmarshal(data, &tmpl)
	require.Nil(err, "error unmarshal template")

	require.Equal("template1", tmpl.Name, "unexpected template name")
	require.Equal("none", tmpl.Index, "unexpected index")
	require.Equal(
		"Simple template used to check bindings and resolver",
		tmpl.Description,
		"unexpected description",
	)

	keys := make([]string, 0, len(tmpl.Bindings))
	for k := range tmpl.Bindings {
		keys = append(keys, k)
	}
	require.ElementsMatch(
		[]string{"Latin", "Greek"}, keys, "binding keys do not match expected values")

	latinTerms, err := tmpl.Bindings["Latin"].Terms()
	require.Nil(err, "error loading terms from Latin bindingi (file)")
	require.ElementsMatch(latin, latinTerms, "Latin terms do not match expected values")

	greekTerms, err := tmpl.Bindings["Greek"].Terms()
	require.Nil(err, "error loading terms from Greek binding")
	require.ElementsMatch(greek, greekTerms, "Greek terms do not match expected values")
}

// Unit test for file-based bindings, independent of template parsing.
func TestFileBindings(t *testing.T) {
	require := require.New(t)

	bnd := Binding{
		Source: "file",
		Path:   "testdata/latin.txt",
	}

	terms, err := bnd.Terms()
	require.Nil(err, "failed to load terms from file")
	require.ElementsMatch(latin, terms, "terms does not match expected values")
}
