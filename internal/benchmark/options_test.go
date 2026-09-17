package benchmark

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func TestRequestOptionsValidate(t *testing.T) {
	t.Parallel()

	base := func() RequestOptions {
		return RequestOptions{
			NumClients:        4,
			RequestsPerClient: ptr(10),
			RepeatEachRequest: 1,
		}
	}

	tests := []struct {
		name    string
		mutate  func(*RequestOptions)
		wantErr string
	}{
		{
			name:   "request count is enough",
			mutate: func(*RequestOptions) {},
		},
		{
			name: "timeout alone is enough",
			mutate: func(o *RequestOptions) {
				o.RequestsPerClient = nil
				o.BenchmarkTimeout = ptr(30)
			},
		},
		{
			name: "neither limit set",
			mutate: func(o *RequestOptions) {
				o.RequestsPerClient = nil
			},
			wantErr: "either benchmark-timeout or num-of-requests-per-client must be set",
		},
		{
			name: "negative timeout",
			mutate: func(o *RequestOptions) {
				o.BenchmarkTimeout = ptr(-1)
			},
			wantErr: "timeout must be zero or greater",
		},
		{
			name: "zero clients",
			mutate: func(o *RequestOptions) {
				o.NumClients = 0
			},
			wantErr: "num clients must be greater than zero",
		},
		{
			name: "negative requests per client",
			mutate: func(o *RequestOptions) {
				o.RequestsPerClient = ptr(-5)
			},
			wantErr: "requests per client must be zero or greater",
		},
		{
			name: "request timeout is optional",
			mutate: func(o *RequestOptions) {
				o.RequestTimeout = ptr(5)
			},
		},
		{
			name: "negative request timeout",
			mutate: func(o *RequestOptions) {
				o.RequestTimeout = ptr(-1)
			},
			wantErr: "request timeout must be zero or greater",
		},
		{
			name: "zero repeats",
			mutate: func(o *RequestOptions) {
				o.RepeatEachRequest = 0
			},
			wantErr: "repeat each request must be greater than zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := base()
			tc.mutate(&opts)

			err := opts.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestTotalRequestsMultipliesClientsRequestsAndRepeats(t *testing.T) {
	t.Parallel()

	opts := RequestOptions{NumClients: 4, RequestsPerClient: ptr(10), RepeatEachRequest: 3}

	total := opts.TotalRequests()

	require.NotNil(t, total)
	require.Equal(t, 120, *total)
}

func TestTotalRequestsIsNilForDurationBoundedRuns(t *testing.T) {
	t.Parallel()

	opts := RequestOptions{NumClients: 4, RepeatEachRequest: 1, BenchmarkTimeout: ptr(30)}

	require.Nil(t, opts.TotalRequests())
}

func TestBenchmarkTimeoutDuration(t *testing.T) {
	t.Parallel()

	require.Nil(t, RequestOptions{}.BenchmarkTimeoutDuration())

	got := RequestOptions{BenchmarkTimeout: ptr(90)}.BenchmarkTimeoutDuration()
	require.NotNil(t, got)
	require.Equal(t, 90*time.Second, *got)
}

func TestResolveTimeout(t *testing.T) {
	t.Parallel()

	require.Zero(t, ResolveTimeout(nil))
	require.Equal(t, 5*time.Second, ResolveTimeout(ptr(5)))
}
