package benchmark

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// authRecorder captures the Authorization header of the last request.
func authRecorder(t *testing.T) (*httptest.Server, *string) {
	t.Helper()

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	return srv, &got
}

func TestNewHTTPClientWithoutCredentialsSendsNoAuthHeader(t *testing.T) {
	t.Parallel()

	srv, got := authRecorder(t)

	client := Client{HTTPClient: HTTPClientConfig{NumClients: 1}.Build(), NodeURL: srv.URL, Index: "idx"}
	_, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.NoError(t, err)
	require.Empty(t, *got)
}

func TestNewHTTPClientWithCredentialsSendsBasicAuth(t *testing.T) {
	t.Parallel()

	srv, got := authRecorder(t)

	client := Client{HTTPClient: HTTPClientConfig{NumClients: 1, Credentials: "user:secret"}.Build(), NodeURL: srv.URL, Index: "idx"}
	_, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.NoError(t, err)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("user:secret")), *got)
}

func TestHTTPClientConfigSizesConnectionPoolToClientCount(t *testing.T) {
	t.Parallel()

	client := HTTPClientConfig{NumClients: 32}.Build()

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "expected a plain transport when no credentials are set")
	require.Equal(t, 32, transport.MaxIdleConnsPerHost)
	require.Equal(t, 32, transport.MaxConnsPerHost)
}

func TestHTTPClientConfigWrapsTransportWhenCredentialsSet(t *testing.T) {
	t.Parallel()

	client := HTTPClientConfig{NumClients: 4, Credentials: "user:secret"}.Build()

	auth, ok := client.Transport.(*authTransport)
	require.True(t, ok, "expected the auth transport when credentials are set")

	inner, ok := auth.transport.(*http.Transport)
	require.True(t, ok)
	require.Equal(t, 4, inner.MaxConnsPerHost)
}

// innerTransport returns the transport a built client sends through, past the
// auth wrapper when there is one.
func innerTransport(t *testing.T, client *http.Client) *http.Transport {
	t.Helper()

	if auth, ok := client.Transport.(*authTransport); ok {
		inner, ok := auth.transport.(*http.Transport)
		require.True(t, ok)
		return inner
	}

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	return transport
}

func TestHTTPClientConfigVerifiesCertificatesByDefault(t *testing.T) {
	t.Parallel()

	configs := map[string]HTTPClientConfig{
		"without credentials": {NumClients: 1},
		"with credentials":    {NumClients: 1, Credentials: "user:secret"},
	}

	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport := innerTransport(t, config.Build())

			require.Nil(t, transport.TLSClientConfig, "verification must stay on until it is turned off explicitly")
		})
	}
}

func TestHTTPClientConfigSkipsVerificationWhenAsked(t *testing.T) {
	t.Parallel()

	transport := innerTransport(t, HTTPClientConfig{NumClients: 1, InsecureSkipTLSVerify: true}.Build())

	require.NotNil(t, transport.TLSClientConfig)
	require.True(t, transport.TLSClientConfig.InsecureSkipVerify)
}

// tlsNode serves over HTTPS with a self-signed certificate, as a benchmark
// target typically does.
func tlsNode(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestHTTPClientRejectsASelfSignedCertificateByDefault(t *testing.T) {
	t.Parallel()

	srv := tlsNode(t)
	client := Client{
		HTTPClient: HTTPClientConfig{NumClients: 1, Credentials: "user:secret"}.Build(),
		NodeURL:    srv.URL,
		Index:      "idx",
	}

	_, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.ErrorContains(t, err, "certificate")
}

func TestHTTPClientAcceptsASelfSignedCertificateWhenVerificationIsSkipped(t *testing.T) {
	t.Parallel()

	srv := tlsNode(t)
	client := Client{
		HTTPClient: HTTPClientConfig{NumClients: 1, InsecureSkipTLSVerify: true}.Build(),
		NodeURL:    srv.URL,
		Index:      "idx",
	}

	s, err := client.Search(context.Background(), testQuery(t, testTemplate("t")))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, s.Status)
}
