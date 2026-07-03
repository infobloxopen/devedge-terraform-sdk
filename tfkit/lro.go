package tfkit

import (
	"context"
	"fmt"
	"time"
)

// Operation is a decoded long-running-operation status. Done and Name are
// lifted from well-known fields; Raw carries the full payload.
type Operation struct {
	Name string
	Done bool
	Raw  map[string]any
}

// PollOptions tunes [Client.PollOperation].
type PollOptions struct {
	// DoneField is the boolean payload field signaling completion (default
	// "done").
	DoneField string
	// Interval is the delay between polls (default 1s).
	Interval time.Duration
	// Timeout bounds the whole poll (default 5m). Zero uses the default; use a
	// context deadline for finer control.
	Timeout time.Duration
}

// PollOption mutates [PollOptions].
type PollOption func(*PollOptions)

// WithInterval sets the poll interval.
func WithInterval(d time.Duration) PollOption { return func(o *PollOptions) { o.Interval = d } }

// WithTimeout sets the overall poll timeout.
func WithTimeout(d time.Duration) PollOption { return func(o *PollOptions) { o.Timeout = d } }

// WithDoneField overrides the completion field name.
func WithDoneField(name string) PollOption { return func(o *PollOptions) { o.DoneField = name } }

// PollOperation GETs opPath repeatedly through the client until the operation
// reports done (per the configured field) or the timeout/context elapses. It is
// engine-agnostic: any operations endpoint returning a JSON object with a
// boolean "done" works. A generated resource uses it to wait out an AIP-151
// long-running create/update.
func (c *Client) PollOperation(ctx context.Context, opPath string, opts ...PollOption) (*Operation, error) {
	o := PollOptions{DoneField: "done", Interval: time.Second, Timeout: 5 * time.Minute}
	for _, fn := range opts {
		fn(&o)
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	for {
		var raw map[string]any
		if err := c.DoRead(ctx, opPath, &raw); err != nil {
			return nil, fmt.Errorf("poll %s: %w", opPath, err)
		}
		op := &Operation{Raw: raw}
		if v, ok := raw["name"].(string); ok {
			op.Name = v
		}
		if v, ok := raw[o.DoneField].(bool); ok {
			op.Done = v
		}
		if op.Done {
			return op, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("operation %s not done before timeout: %w", opPath, ctx.Err())
		case <-time.After(o.Interval):
		}
	}
}
