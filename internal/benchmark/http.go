package benchmark

import (
	"crypto/tls"
	"encoding/base64"
	"net/http"
)

// HTTPClientConfig builds the shared client every worker sends through. The
// connection pool is sized to the client count so workers do not contend for
// connections and skew the measurements.
type HTTPClientConfig struct {
	NumClients int

	// Credentials are basic auth as "user:password". Empty sends no
	// Authorization header.
	Credentials string

	// InsecureSkipTLSVerify turns off certificate verification.
	InsecureSkipTLSVerify bool
}

func (c HTTPClientConfig) Build() *http.Client {
	transport := &http.Transport{
		MaxIdleConnsPerHost: c.NumClients,
		MaxConnsPerHost:     c.NumClients,
	}

	if c.InsecureSkipTLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in, for benchmark targets using self-signed certs
	}

	if c.Credentials == "" {
		return &http.Client{Transport: transport}
	}
	return &http.Client{
		Transport: &authTransport{
			credentials: c.Credentials,
			transport:   transport,
		},
	}
}

type authTransport struct {
	credentials string
	transport   http.RoundTripper
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(a.credentials))
	req.Header.Set("Authorization", "Basic "+encoded)
	return a.transport.RoundTrip(req)
}
