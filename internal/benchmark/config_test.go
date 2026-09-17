package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareQuerySupportsRawJSONFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rawPath := filepath.Join(dir, "raw.json")
	err := os.WriteFile(rawPath, []byte(`{"query":{"match_all":{}}}`), 0o644)
	require.NoError(t, err)

	tmpl, err := PrepareQuery(QueryConfig{
		TemplatePath:    rawPath,
		ExtraFieldsJSON: `{"size":10}`,
	})
	require.NoError(t, err)
	require.Equal(t, "raw.json", tmpl.Name)
	require.Empty(t, tmpl.Bindings)

	var body map[string]any
	require.NoError(t, json.Unmarshal(tmpl.Template, &body))
	require.Contains(t, body, "query")
	require.Equal(t, float64(10), body["size"])
}

func TestPrepareQuerySupportsTemplateFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.json")
	err := os.WriteFile(templatePath, []byte(`{
		"name": "fixture-template",
		"bindings": {
			"Term": {
				"source": "inline",
				"terms": ["one", "two"]
			}
		},
		"template": {
			"query": {
				"term": {
					"value": "{{.Term}}"
				}
			}
		}
	}`), 0o644)
	require.NoError(t, err)

	tmpl, err := PrepareQuery(QueryConfig{
		TemplatePath:    templatePath,
		ExtraFieldsJSON: `{"track_total_hits":true}`,
	})
	require.NoError(t, err)
	require.Equal(t, "fixture-template", tmpl.Name)
	require.Contains(t, tmpl.Bindings, "Term")

	var body map[string]any
	require.NoError(t, json.Unmarshal(tmpl.Template, &body))
	require.Contains(t, body, "query")
	require.Equal(t, true, body["track_total_hits"])
}
