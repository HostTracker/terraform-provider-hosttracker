// Package client adapts the HostTracker Go SDK to what a Terraform provider
// needs: RFC 9457 problem documents rendered as Terraform diagnostics, PATCH
// bodies built from the members that actually changed, and cursor pages
// drained into slices.
//
// Monitor bodies travel as raw JSON maps rather than the SDK's generated
// request structs. The monitor `settings` object is a 14-branch union whose
// branch is chosen by the monitor's type, and a PATCH must be able to say
// "this member is absent" and "this member is explicitly null" - both of
// which a struct of pointers cannot express for every case. The reference
// reads (locations, monitor types, account) use the SDK's generated views,
// which have no such requirement.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
	"github.com/google/uuid"
)

// DefaultBaseURL is the production API root.
const DefaultBaseURL = "https://api2.host-tracker.com"

// Config is everything the provider block configures.
type Config struct {
	Token     string
	BaseURL   string
	Timeout   time.Duration
	RetryMax  int
	UserAgent string
}

// Client is the provider's view of the API.
type Client struct {
	api     *hosttracker.Client
	baseURL string
}

// New builds a client. An empty token yields an anonymous client, which is
// enough for the reference-tier reads but for nothing else.
func New(cfg Config) (*Client, error) {
	opts := []hosttracker.Option{}
	if cfg.BaseURL != "" {
		opts = append(opts, hosttracker.WithBaseURL(cfg.BaseURL))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, hosttracker.WithTimeout(cfg.Timeout))
	}
	if cfg.RetryMax > 0 {
		opts = append(opts, hosttracker.WithMaxRetries(cfg.RetryMax))
	}
	if cfg.UserAgent != "" {
		opts = append(opts, hosttracker.WithUserAgent(cfg.UserAgent))
	}
	api, err := hosttracker.New(cfg.Token, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{api: api, baseURL: api.BaseURL()}, nil
}

// API exposes the underlying SDK client for the reads that use its
// generated views.
func (c *Client) API() *hosttracker.Client { return c.api }

// BaseURL is the API root this client talks to.
func (c *Client) BaseURL() string { return c.baseURL }

// Monitor is the wire shape of a monitor, with `settings` left as a map so
// that a member this release does not know about survives a round trip.
type Monitor struct {
	ID           string         `json:"id"`
	Type         string         `json:"type,omitempty"`
	Name         *string        `json:"name,omitempty"`
	URL          string         `json:"url,omitempty"`
	EffectiveURL *string        `json:"effectiveUrl,omitempty"`
	State        *string        `json:"state,omitempty"`
	Since        int64          `json:"since,omitempty"`
	Enabled      bool           `json:"enabled"`
	Tags         []string       `json:"tags,omitempty"`
	Updated      int64          `json:"updated,omitempty"`
	Created      *int64         `json:"created,omitempty"`
	OpenStat     bool           `json:"openStat"`
	FullLog      bool           `json:"fullLog"`
	CronSchedule *string        `json:"cronSchedule,omitempty"`
	Interval     *int64         `json:"interval,omitempty"`
	SLATarget    *float64       `json:"slaTarget,omitempty"`
	Locations    *Locations     `json:"locations,omitempty"`
	Recheck      *Recheck       `json:"recheck,omitempty"`
	Settings     map[string]any `json:"settings,omitempty"`
}

// Locations is where a monitor is checked from.
type Locations struct {
	Pools          []string `json:"pools,omitempty"`
	Fallback       *string  `json:"fallback,omitempty"`
	ExcludedAgents []string `json:"excludedAgents,omitempty"`
}

// Recheck is the quorum rule applied before a failure concludes.
type Recheck struct {
	Strategy   *string `json:"strategy,omitempty"`
	MinNumDown *int64  `json:"minNumDown,omitempty"`
}

// ErrNotFound reports a monitor that is gone, or was never this account's.
var ErrNotFound = errors.New("not found")

// CreateMonitor posts a monitor and returns it as a read renders it.
// The create response does not publish `effectiveUrl`, so the monitor is
// re-read before it is handed back.
func (c *Client) CreateMonitor(ctx context.Context, body map[string]any) (*Monitor, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.CreateMonitorWithBodyWithResponse(ctx, nil, "application/json", bytesReader(raw))
	if err != nil {
		return nil, err
	}
	var created Monitor
	if err := json.Unmarshal(resp.Body, &created); err != nil {
		return nil, fmt.Errorf("decoding the created monitor: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("the create answered %d without a monitor id", statusOf(resp.HTTPResponse))
	}
	return c.GetMonitor(ctx, created.ID)
}

// GetMonitor reads one monitor with its settings. A monitor that is gone
// answers ErrNotFound.
func (c *Client) GetMonitor(ctx context.Context, id string) (*Monitor, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%q is not a monitor id: %w", id, err)
	}
	expand := []hosttracker.GetMonitorParamsExpand{"settings"}
	resp, err := c.api.GetMonitorWithResponse(ctx, parsed, &hosttracker.GetMonitorParams{Expand: &expand})
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var m Monitor
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return nil, fmt.Errorf("decoding the monitor: %w", err)
	}
	return &m, nil
}

// UpdateMonitor sends a PATCH body and returns the monitor as a read
// renders it. An empty body is a no-op that still re-reads.
func (c *Client) UpdateMonitor(ctx context.Context, id string, body map[string]any) (*Monitor, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%q is not a monitor id: %w", id, err)
	}
	if len(body) > 0 {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		if _, err := c.api.UpdateMonitorWithBodyWithResponse(ctx, parsed, nil, "application/json", bytesReader(raw)); err != nil {
			if hosttracker.IsNotFound(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	return c.GetMonitor(ctx, id)
}

// DeleteMonitor removes a monitor. The API answers a receipt rather than a
// 204; a monitor that is already gone is not an error.
func (c *Client) DeleteMonitor(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%q is not a monitor id: %w", id, err)
	}
	if _, err := c.api.DeleteMonitorWithResponse(ctx, parsed, nil); err != nil {
		if hosttracker.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// MonitorFilter narrows a monitor listing. Every member is optional and
// they are ANDed.
type MonitorFilter struct {
	State []string
	Type  []string
	Tag   []string
	URL   []string
	Name  string
	Q     string
	// Limit caps how many rows are collected across pages. Zero means the
	// package default.
	Limit int
	// Expand asks for extra blocks on each row, `settings` among them.
	Expand []string
}

// DefaultListCap is how many monitors a list data source collects when the
// configuration names no cap of its own.
const DefaultListCap = 1000

// ListMonitors walks the cursor pages and collects the rows, stopping at
// the filter's cap.
func (c *Client) ListMonitors(ctx context.Context, f MonitorFilter) ([]Monitor, error) {
	collected := f.Limit
	if collected <= 0 {
		collected = DefaultListCap
	}
	pageSize := int32(200)
	if collected < int(pageSize) {
		pageSize = int32(collected)
	}

	fetch := func(ctx context.Context, cursor *string) ([]Monitor, *string, error) {
		params := &hosttracker.ListMonitorParams{Cursor: cursor, Limit: &pageSize}
		if len(f.State) > 0 {
			v := make([]hosttracker.ListMonitorParamsState, 0, len(f.State))
			for _, s := range f.State {
				v = append(v, hosttracker.ListMonitorParamsState(s))
			}
			params.State = &v
		}
		if len(f.Type) > 0 {
			v := make([]hosttracker.ListMonitorParamsType, 0, len(f.Type))
			for _, s := range f.Type {
				v = append(v, hosttracker.ListMonitorParamsType(NormalizeType(s)))
			}
			params.Type = &v
		}
		if len(f.Tag) > 0 {
			tags := append([]string(nil), f.Tag...)
			params.Tag = &tags
		}
		if len(f.URL) > 0 {
			urls := append([]string(nil), f.URL...)
			params.Url = &urls
		}
		if f.Q != "" {
			params.Q = &f.Q
		}
		if len(f.Expand) > 0 {
			v := make([]hosttracker.ListMonitorParamsExpand, 0, len(f.Expand))
			for _, s := range f.Expand {
				v = append(v, hosttracker.ListMonitorParamsExpand(s))
			}
			params.Expand = &v
		}

		resp, err := c.api.ListMonitorWithResponse(ctx, params)
		if err != nil {
			return nil, nil, err
		}
		var page struct {
			Data       []Monitor `json:"data"`
			NextCursor *string   `json:"nextCursor"`
		}
		if err := json.Unmarshal(resp.Body, &page); err != nil {
			return nil, nil, fmt.Errorf("decoding the monitor page: %w", err)
		}
		return page.Data, page.NextCursor, nil
	}

	out := make([]Monitor, 0, 16)
	for m, err := range hosttracker.Paginate(ctx, fetch) {
		if err != nil {
			return nil, err
		}
		if f.Name != "" && (m.Name == nil || *m.Name != f.Name) {
			continue
		}
		out = append(out, m)
		if len(out) >= collected {
			break
		}
	}
	return out, nil
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
