package benchmark

import (
	"context"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

func testQuery(t *testing.T, tmpl query.Template) query.Query {
	t.Helper()

	resolver, err := query.NewResolver(tmpl, rand.New(rand.NewSource(1)), true)
	require.NoError(t, err)
	return resolver.Query()
}

func TestClientSearchPopulatesSample(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	client := Client{HTTPClient: srv.Client(), NodeURL: srv.URL, Index: "idx"}

	s, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, s.Status)
	require.JSONEq(t, okResponse, string(s.Body))
	require.Positive(t, s.ClientDuration)
	require.Equal(t, "t", s.Query.Template.Name)
}

func TestClientSearchTargetsTheIndexSearchEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	client := Client{HTTPClient: srv.Client(), NodeURL: srv.URL, Index: "my-index"}
	_, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.NoError(t, err)
	require.Equal(t, "/my-index/_search", gotPath)
	require.Equal(t, http.MethodPost, gotMethod)
	require.JSONEq(t, `{"query":{"match_all":{}}}`, gotBody)
}

func TestClientSearchLatencyIncludesTheBodyRead(t *testing.T) {
	t.Parallel()

	const delay = 150 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"took":3,`)
		w.(http.Flusher).Flush()
		time.Sleep(delay)
		_, _ = io.WriteString(w, `"hits":{"total":{"value":1,"relation":"eq"},"hits":[]}}`)
	}))
	t.Cleanup(srv.Close)

	client := Client{HTTPClient: srv.Client(), NodeURL: srv.URL, Index: "idx"}
	s, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.NoError(t, err)
	require.GreaterOrEqual(t, s.ClientDuration, delay, "client latency must cover the body transfer")
	require.JSONEq(t, `{"took":3,"hits":{"total":{"value":1,"relation":"eq"},"hits":[]}}`, string(s.Body))
}

// hangingNode flushes the response headers and then blocks until the test
// finishes, so a request against it can only end by being cancelled.
func hangingNode(t *testing.T) *httptest.Server {
	t.Helper()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"took":3,`)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestClientSearchStopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()

	srv := hangingNode(t)
	client := Client{HTTPClient: srv.Client(), NodeURL: srv.URL, Index: "idx"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := client.Search(ctx, testQuery(t, testTemplate("t")))

	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), 2*time.Second, "cancellation must not wait for the node")
}

func TestClientSearchStopsAtTheRequestTimeout(t *testing.T) {
	t.Parallel()

	srv := hangingNode(t)
	client := Client{
		HTTPClient: srv.Client(),
		NodeURL:    srv.URL,
		Index:      "idx",
		Timeout:    50 * time.Millisecond,
	}

	start := time.Now()
	_, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	// The headers arrive and only the body stalls, so a limit covering the
	// headers alone would never fire here.
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestClientSearchSendsJSONContentType(t *testing.T) {
	t.Parallel()

	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	client := Client{HTTPClient: srv.Client(), NodeURL: srv.URL, Index: "idx"}
	_, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.NoError(t, err)
	require.Equal(t, "application/json", gotContentType)
}

func TestClientSearchRecordsNonOKStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	t.Cleanup(srv.Close)

	client := Client{HTTPClient: srv.Client(), NodeURL: srv.URL, Index: "idx"}
	s, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	// A node-level error is still a completed measurement, not a client error.
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, s.Status)
}

func TestClientSearchFailsOnUnparseableResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "definitely not json")
	}))
	t.Cleanup(srv.Close)

	client := Client{HTTPClient: srv.Client(), NodeURL: srv.URL, Index: "idx"}
	_, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.Error(t, err)
}

func TestClientSearchFailsWhenNodeUnreachable(t *testing.T) {
	t.Parallel()

	client := Client{
		HTTPClient: HTTPClientConfig{NumClients: 1}.Build(),
		// Port 1 is reserved and will refuse the connection.
		NodeURL: "http://127.0.0.1:1",
		Index:   "idx",
	}
	_, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.Error(t, err)
}
