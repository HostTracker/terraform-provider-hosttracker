package client

import (
	"context"
	"encoding/json"
	"fmt"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
)

// Maintenance is the wire shape of a maintenance window.
type Maintenance struct {
	ID          string                 `json:"id"`
	Name        *string                `json:"name,omitempty"`
	From        int64                  `json:"from,omitempty"`
	To          int64                  `json:"to,omitempty"`
	DurationSec int64                  `json:"durationSec,omitempty"`
	Timezone    *string                `json:"timezone,omitempty"`
	Recurrence  *MaintenanceRecurrence `json:"recurrence,omitempty"`
	Enabled     bool                   `json:"enabled"`
	State       *string                `json:"state,omitempty"`
	Overlimited bool                   `json:"overlimited"`
	Suppress    *Suppress              `json:"suppress,omitempty"`
	MonitorIDs  []string               `json:"monitorIds,omitempty"`
	Monitors    []MaintenanceMonitor   `json:"monitors,omitempty"`
	Created     int64                  `json:"created,omitempty"`
	Updated     int64                  `json:"updated,omitempty"`
}

// MaintenanceRecurrence is how a window repeats.
type MaintenanceRecurrence struct {
	WeekDays []string `json:"weekDays,omitempty"`
}

// Suppress is what a window holds back while it is active.
type Suppress struct {
	Alerts bool `json:"alerts"`
	Stats  bool `json:"stats"`
}

// MaintenanceMonitor is one monitor a window covers, with the suppression
// that monitor carries.
type MaintenanceMonitor struct {
	MonitorID string    `json:"monitorId"`
	Suppress  *Suppress `json:"suppress,omitempty"`
}

// CreateMaintenance posts a window and returns it as a read renders it.
func (c *Client) CreateMaintenance(ctx context.Context, body map[string]any) (*Maintenance, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.CreateMaintenanceWithBodyWithResponse(ctx, nil, "application/json", bytesReader(raw))
	if err != nil {
		return nil, err
	}
	var created Maintenance
	if err := json.Unmarshal(resp.Body, &created); err != nil {
		return nil, fmt.Errorf("decoding the created maintenance window: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("the create answered %d without a window id", statusOf(resp.HTTPResponse))
	}
	return c.GetMaintenance(ctx, created.ID)
}

// GetMaintenance reads one window. One that is gone answers ErrNotFound.
func (c *Client) GetMaintenance(ctx context.Context, id string) (*Maintenance, error) {
	parsed, err := parseID(id, "maintenance window")
	if err != nil {
		return nil, err
	}
	resp, err := c.api.GetMaintenanceWithResponse(ctx, parsed, &hosttracker.GetMaintenanceParams{})
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var window Maintenance
	if err := json.Unmarshal(resp.Body, &window); err != nil {
		return nil, fmt.Errorf("decoding the maintenance window: %w", err)
	}
	return &window, nil
}

// UpdateMaintenance sends a PATCH body and re-reads the window.
func (c *Client) UpdateMaintenance(ctx context.Context, id string, body map[string]any) (*Maintenance, error) {
	parsed, err := parseID(id, "maintenance window")
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		if _, err := c.api.UpdateMaintenanceWithBodyWithResponse(ctx, parsed, nil, "application/json", bytesReader(raw)); err != nil {
			if hosttracker.IsNotFound(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	return c.GetMaintenance(ctx, id)
}

// DeleteMaintenance removes a window. One that is already gone is not an
// error.
func (c *Client) DeleteMaintenance(ctx context.Context, id string) error {
	parsed, err := parseID(id, "maintenance window")
	if err != nil {
		return err
	}
	if _, err := c.api.DeleteMaintenanceWithResponse(ctx, parsed, nil); err != nil {
		if hosttracker.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// MaintenanceFilter narrows a window listing. From and To bound the
// window's START, not its extent.
type MaintenanceFilter struct {
	From    int64
	To      int64
	State   []string
	Monitor []string
	Name    string
	Limit   int
}

// ListMaintenances walks the cursor pages and collects the windows,
// stopping at the filter's cap.
func (c *Client) ListMaintenances(ctx context.Context, f MaintenanceFilter) ([]Maintenance, error) {
	collected := f.Limit
	if collected <= 0 {
		collected = DefaultListCap
	}
	pageSize := int32(200)
	if collected < int(pageSize) {
		pageSize = int32(collected)
	}

	fetch := func(ctx context.Context, cursor *string) ([]Maintenance, *string, error) {
		params := &hosttracker.ListMaintenanceParams{Cursor: cursor, Limit: &pageSize}
		if f.From != 0 {
			from := f.From
			params.From = &from
		}
		if f.To != 0 {
			to := f.To
			params.To = &to
		}
		if len(f.State) > 0 {
			v := make([]hosttracker.ListMaintenanceParamsState, 0, len(f.State))
			for _, s := range f.State {
				v = append(v, hosttracker.ListMaintenanceParamsState(s))
			}
			params.State = &v
		}
		if len(f.Monitor) > 0 {
			monitors := append([]string(nil), f.Monitor...)
			params.Monitor = &monitors
		}
		resp, err := c.api.ListMaintenanceWithResponse(ctx, params)
		if err != nil {
			return nil, nil, err
		}
		var page struct {
			Data       []Maintenance `json:"data"`
			NextCursor *string       `json:"nextCursor"`
		}
		if err := json.Unmarshal(resp.Body, &page); err != nil {
			return nil, nil, fmt.Errorf("decoding the maintenance page: %w", err)
		}
		return page.Data, page.NextCursor, nil
	}

	out := make([]Maintenance, 0, 16)
	for window, err := range hosttracker.Paginate(ctx, fetch) {
		if err != nil {
			return nil, err
		}
		if f.Name != "" && (window.Name == nil || *window.Name != f.Name) {
			continue
		}
		out = append(out, window)
		if len(out) >= collected {
			break
		}
	}
	return out, nil
}
