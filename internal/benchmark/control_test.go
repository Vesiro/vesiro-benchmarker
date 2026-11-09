package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/stretchr/testify/require"
)

func TestControlExcludesWarmupAndAcknowledgmentDelays(t *testing.T) {
	for _, sequential := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "folder"}[sequential], func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			var served atomic.Int64
			config := testRunConfig("http://node", countedOptions(1, 2, 1), testTemplate("a"))
			config.HTTPClient.Transport = warmupTransport(func(r *http.Request) (*http.Response, error) {
				served.Add(1)
				select {
				case <-time.After(10 * time.Millisecond):
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(okResponse)), Header: make(http.Header)}, nil
			})
			summary := &publisher.Summary{Out: io.Discard, PhaseTiming: sequential}
			result := make(chan error, 1)
			go func() {
				decoder := json.NewDecoder(server)
				for _, event := range []string{"ready", "finished"} {
					var msg boundary
					if err := decoder.Decode(&msg); err != nil {
						result <- err
						return
					}
					if msg.Event != event || msg.Phase != 1 || msg.Template != "a" {
						result <- errors.New("wrong boundary")
						return
					}
					if event == "ready" && served.Load() < 20 {
						result <- errors.New("warmup not finished")
						return
					}
					time.Sleep(150 * time.Millisecond)
					msg.Event = "ack"
					if err := json.NewEncoder(server).Encode(msg); err != nil {
						result <- err
						return
					}
				}
				result <- nil
			}()
			require.NoError(t, Execute(context.Background(), ExecutionConfig{Control: &Control{conn: client}, RunConfig: config, Publishers: []publisher.Publisher{summary}, Sequential: sequential, Warmup: 1}))
			require.NoError(t, <-result)
			require.Equal(t, 2, summary.Calculate().Requests)
			require.Less(t, summary.Calculate().ExecutionTimeMs, 150.0, "controller waits must not affect QPS")
		})
	}
}

func TestControlRejectsInvalidAckAndCancellation(t *testing.T) {
	for _, kind := range []string{"version", "phase", "event", "template", "error", "disconnect", "timeout", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			joined := make(chan struct{})
			go func() {
				defer close(joined)
				var msg boundary
				_ = json.NewDecoder(server).Decode(&msg)
				msg.Event = "ack"
				switch kind {
				case "version":
					msg.Version = 2
				case "phase":
					msg.Phase++
				case "event":
					msg.Event = "ready"
				case "template":
					msg.Template = "wrong"
				case "error":
					msg.Error = "collector failed"
				case "disconnect":
					server.Close()
					return
				case "timeout":
					<-ctx.Done()
					return
				case "cancel":
					cancel()
					return
				}
				_ = json.NewEncoder(server).Encode(msg)
			}()
			err := (&Control{conn: client}).exchange(ctx, "ready", "a", nil, 0)
			require.Error(t, err)
			<-joined
		})
	}
}

func TestPublisherFailureJoinsWorkersBeforeAbortedBoundary(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	config := testRunConfig("http://node", countedOptions(8, 100, 1))
	var active atomic.Int64
	config.HTTPClient.Transport = warmupTransport(func(r *http.Request) (*http.Response, error) {
		active.Add(1)
		defer active.Add(-1)
		select {
		case <-time.After(time.Millisecond):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(okResponse)), Header: make(http.Header)}, nil
	})
	result := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(server)
		for _, event := range []string{"ready", "aborted"} {
			var msg boundary
			if err := decoder.Decode(&msg); err != nil {
				result <- err
				return
			}
			if msg.Event != event {
				result <- errors.New("wrong outcome")
				return
			}
			if event == "aborted" && active.Load() != 0 {
				result <- errors.New("workers still active")
				return
			}
			msg.Event = "ack"
			msg.Error = ""
			if err := json.NewEncoder(server).Encode(msg); err != nil {
				result <- err
				return
			}
		}
		result <- nil
	}()
	sentinel := errors.New("publisher failed")
	err := Execute(context.Background(), ExecutionConfig{Control: &Control{conn: client}, RunConfig: config, Publishers: []publisher.Publisher{&recordingPublisher{publishErr: sentinel}}, Sequential: true})
	require.ErrorIs(t, err, sentinel)
	require.NoError(t, <-result)
}

func TestFinalJSONRecordsPublisherFinishFailure(t *testing.T) {
	srv, _ := mockNode(t)
	sentinel := errors.New("publisher finish failed")
	var out bytes.Buffer
	summary := &publisher.Summary{Out: &out, Format: publisher.OutputJSON}
	err := Execute(context.Background(), ExecutionConfig{RunConfig: testRunConfig(srv.URL, countedOptions(1, 2, 1)), Publishers: []publisher.Publisher{&recordingPublisher{finishErr: sentinel}, summary}})
	require.ErrorIs(t, err, sentinel)
	var report publisher.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	require.Contains(t, report.Error, sentinel.Error())
	require.Equal(t, summary.Report().Error, report.Error)
}
