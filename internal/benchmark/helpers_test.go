package benchmark

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

const okResponse = `{"took":3,"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_id":"d1","_score":1.5}]}}`

// testTemplate returns a binding-free template that renders to a fixed body.
func testTemplate(name string) query.Template {
	return query.Template{
		Name:     name,
		Bindings: map[string]query.Binding{},
		Template: json.RawMessage(`{"query":{"match_all":{}}}`),
	}
}

// testTemplateWithTerms returns a template whose one binding varies per query,
// so callers can tell generated queries apart.
func testTemplateWithTerms(name string, terms ...string) query.Template {
	return query.Template{
		Name: name,
		Bindings: map[string]query.Binding{
			"Term": {Source: "inline", TermsInline: terms},
		},
		Template: json.RawMessage(`{"query":{"match":{"content":"{{.Term}}"}}}`),
	}
}

// mockNode serves a valid search response and counts how many it served.
func mockNode(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	return srv, &served
}

func testRunConfig(nodeURL string, opts RequestOptions, templates ...query.Template) RunConfig {
	if len(templates) == 0 {
		templates = []query.Template{testTemplate("t")}
	}
	return RunConfig{
		RequestOptions:      opts,
		HTTPClient:          HTTPClientConfig{NumClients: opts.NumClients}.Build(),
		NodeURL:             nodeURL,
		IndexName:           "idx",
		Templates:           templates,
		Seed:                42,
		SkipQueryValidation: true,
	}
}

// countedOptions asks for a fixed number of requests per client.
func countedOptions(clients, perClient, repeat int) RequestOptions {
	return RequestOptions{
		NumClients:        clients,
		RequestsPerClient: &perClient,
		RepeatEachRequest: repeat,
		Progress:          "none",
	}
}

// timedOptions runs until the given number of seconds elapses.
func timedOptions(clients, seconds int) RequestOptions {
	return RequestOptions{
		NumClients:        clients,
		RepeatEachRequest: 1,
		BenchmarkTimeout:  &seconds,
		Progress:          "none",
	}
}
