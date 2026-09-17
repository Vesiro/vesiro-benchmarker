package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

type Client struct {
	HTTPClient *http.Client
	NodeURL    string
	Index      string

	// Timeout bounds a single request, body read included. Zero leaves it
	// bounded only by the context passed to Search.
	Timeout time.Duration
}

func (c *Client) Search(ctx context.Context, q query.Query) (sample.Sample, error) {
	url := fmt.Sprintf("%s/%s/_search", c.NodeURL, c.Index)
	requestBody, err := q.Json()
	if err != nil {
		return sample.Sample{}, err
	}

	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		return sample.Sample{}, err
	}
	request.Header.Set("Content-Type", "application/json")

	// Measure time taken for the request. Does not need to be UTC since golang
	// time.Time uses monotonic clock for duration measurement.
	t0 := time.Now()

	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return sample.Sample{}, err
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	clientDuration := time.Since(t0)
	if err != nil {
		return sample.Sample{}, err
	}

	if !json.Valid(responseBody) {
		return sample.Sample{}, fmt.Errorf("response body is not valid json")
	}

	return sample.Sample{
		Query:          q,
		ClientDuration: clientDuration,
		Status:         response.StatusCode,
		Body:           responseBody,
	}, nil
}
