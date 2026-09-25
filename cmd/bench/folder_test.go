package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

// writeQueryTree creates a query folder with a nested layout, including a file
// name that repeats across subfolders.
func writeQueryTree(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	for _, rel := range []string{
		"top.json",
		"boolean/term.json",
		"boolean/nested/deep.json",
		"terms/term.json",
	} {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(`{"query":{"match_all":{}}}`), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "empty"), 0o755))

	return root
}

func templateNames(templates []query.Template) []string {
	names := make([]string, 0, len(templates))
	for _, tmpl := range templates {
		names = append(names, tmpl.Name)
	}
	return names
}

func TestLoadQueryFolderIncludesEverySubfolder(t *testing.T) {
	t.Parallel()

	cmd := FolderCmd{QueryFolder: writeQueryTree(t)}

	templates, err := cmd.loadQueryFolder()

	require.NoError(t, err)
	require.Equal(t, []string{
		"boolean/nested/deep.json",
		"boolean/term.json",
		"terms/term.json",
		"top.json",
	}, templateNames(templates), "names are relative paths, so same-named files stay apart")
}

func TestLoadQueryFolderReportsTheNestedFileThatFailed(t *testing.T) {
	t.Parallel()

	root := writeQueryTree(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "boolean", "broken.json"), []byte(`{`), 0o644))
	cmd := FolderCmd{QueryFolder: root}

	_, err := cmd.loadQueryFolder()

	require.ErrorContains(t, err, filepath.Join("boolean", "broken.json"))
}

func TestLoadQueryFolderWithOnlySubfolders(t *testing.T) {
	t.Parallel()

	root := writeQueryTree(t)
	require.NoError(t, os.Remove(filepath.Join(root, "top.json")))

	templates, err := (&FolderCmd{QueryFolder: root}).loadQueryFolder()

	require.NoError(t, err)
	require.Len(t, templates, 3)
}

func TestLoadQueryFolderWithNoFilesAnywhereFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b"), 0o755))

	_, err := (&FolderCmd{QueryFolder: root}).loadQueryFolder()

	require.ErrorContains(t, err, "no query files found")
}

func TestLongestNameMeasuresTheWidestTemplateName(t *testing.T) {
	t.Parallel()

	templates := []query.Template{{Name: "a.json"}, {Name: "multi-term/fuzzy.json"}, {Name: "b.json"}}

	require.Equal(t, len("multi-term/fuzzy.json"), longestName(templates))
	require.Zero(t, longestName(nil))
}

func TestFolderWarmupFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want int
		err  string
	}{
		{name: "disabled by default"},
		{name: "seconds", args: []string{"--warmup=10"}, want: 10},
		{name: "zero", args: []string{"--warmup=0"}},
		{name: "negative", args: []string{"--warmup=-1"}, err: "warmup must be zero or greater"},
		{name: "random", args: []string{"--warmup=10", "--random"}, want: 10},
		{name: "client validation retained", args: []string{"--warmup=10", "--num-clients=0"}, err: "num clients must be greater than zero"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cli CLI
			parser, err := kong.New(&cli)
			require.NoError(t, err)
			args := []string{"folder", "--node-url=http://localhost:9200", "--index-name=idx", "--query-folder=queries", "--requests-per-client=1"}
			_, err = parser.Parse(append(args, tc.args...))
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, cli.Folder.Warmup)
		})
	}
}
