package benchmark

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Control is a dedicated measurement-boundary channel, separate from reports.
type Control struct {
	conn   net.Conn
	reader *bufio.Reader
	phase  uint64
}
type boundary struct {
	Version  int     `json:"version"`
	Event    string  `json:"event"`
	Phase    uint64  `json:"phase"`
	Template string  `json:"template"`
	Error    string  `json:"error,omitempty"`
	Duration float64 `json:"duration_seconds,omitempty"`
}

func DialControl(ctx context.Context, path string) (*Control, error) {
	conn, err := (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("measurement control: %w", err)
	}
	return &Control{conn: conn}, nil
}
func (c *Control) Close() error { return c.conn.Close() }
func (c *Control) exchange(ctx context.Context, event, name string, runErr error, duration time.Duration) error {
	if c == nil {
		return nil
	}
	if event == "ready" {
		c.phase++
	}
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return err
	}
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-ctx.Done():
			c.conn.SetDeadline(time.Now())
		case <-done:
		}
	}()
	defer func() { close(done); <-exited }()
	// done is closed by the joined cleanup above.
	msg := boundary{Version: 1, Event: event, Phase: c.phase, Template: name, Duration: duration.Seconds()}
	if runErr != nil {
		msg.Error = runErr.Error()
	}
	if err := json.NewEncoder(c.conn).Encode(msg); err != nil {
		return fmt.Errorf("measurement control send: %w", err)
	}
	if c.reader == nil {
		c.reader = bufio.NewReader(c.conn)
	}
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("measurement control acknowledgment: %w", err)
	}
	var ack boundary
	if err := json.Unmarshal(line, &ack); err != nil {
		return fmt.Errorf("measurement control acknowledgment: %w", err)
	}
	if ack.Version != 1 || ack.Event != "ack" || ack.Phase != c.phase || ack.Template != name {
		return fmt.Errorf("incompatible or out-of-order measurement acknowledgment: %+v", ack)
	}
	if ack.Error != "" {
		return fmt.Errorf("measurement controller: %s", ack.Error)
	}
	return nil
}
