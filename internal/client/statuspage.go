package client

import (
	"context"
	"encoding/json"
	"fmt"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
	"github.com/google/uuid"
)

// StatusPageHost is the multi-tenant origin a page is published from when
// it carries no custom domain of its own. The API does not publish the
// page's public address, so the provider composes it from the slug.
const StatusPageHost = "status.host-tracker.com"

// StatusPage is the wire shape of a status page. Settings travel as a map
// so that a member this release does not know about survives a round trip
// and so that a PATCH can distinguish an absent member from a null one.
type StatusPage struct {
	ID                  string                `json:"id"`
	Slug                string                `json:"slug,omitempty"`
	Title               string                `json:"title,omitempty"`
	ComponentCount      int64                 `json:"componentCount,omitempty"`
	UnresolvedIncidents int64                 `json:"unresolvedIncidents,omitempty"`
	HasPassword         bool                  `json:"hasPassword"`
	Created             int64                 `json:"created,omitempty"`
	CustomDomain        *string               `json:"customDomain,omitempty"`
	Settings            map[string]any        `json:"settings,omitempty"`
	Components          []StatusPageComponent `json:"components,omitempty"`
}

// StatusPageComponent is one row of a page's component set. A component is
// either MONITORED (it names a monitor and takes its state from that
// monitor's checks) or THIRD-PARTY (its own name and a pinned state).
type StatusPageComponent struct {
	ID          *string `json:"id,omitempty"`
	MonitorID   *string `json:"monitorId,omitempty"`
	Name        *string `json:"name,omitempty"`
	Group       *string `json:"group,omitempty"`
	ThirdParty  bool    `json:"thirdParty,omitempty"`
	ManualState *string `json:"manualState,omitempty"`
}

// PublicURL is where the page is served from: its custom domain when one
// is configured, and the shared status origin otherwise.
func (p *StatusPage) PublicURL() string {
	if p.CustomDomain != nil && *p.CustomDomain != "" {
		return "https://" + *p.CustomDomain + "/"
	}
	if p.Slug == "" {
		return ""
	}
	return "https://" + StatusPageHost + "/" + p.Slug
}

// CreateStatusPage posts a page, optionally complete with its component
// set, and returns the full view the create answers with.
func (c *Client) CreateStatusPage(ctx context.Context, body map[string]any) (*StatusPage, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.CreateStatusPageWithBodyWithResponse(ctx, nil, "application/json", bytesReader(raw))
	if err != nil {
		return nil, err
	}
	var created StatusPage
	if err := json.Unmarshal(resp.Body, &created); err != nil {
		return nil, fmt.Errorf("decoding the created status page: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("the create answered %d without a status page id", statusOf(resp.HTTPResponse))
	}
	return &created, nil
}

// GetStatusPage reads one page with its settings and component set. A page
// that is gone answers ErrNotFound.
func (c *Client) GetStatusPage(ctx context.Context, id string) (*StatusPage, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%q is not a status page id: %w", id, err)
	}
	resp, err := c.api.GetStatusPageWithResponse(ctx, parsed, nil)
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var page StatusPage
	if err := json.Unmarshal(resp.Body, &page); err != nil {
		return nil, fmt.Errorf("decoding the status page: %w", err)
	}
	return &page, nil
}

// UpdateStatusPage sends a PATCH body. Only `title` and `settings` are
// patchable; the slug is permanent and the component set has a door of its
// own. An empty body re-reads instead.
func (c *Client) UpdateStatusPage(ctx context.Context, id string, body map[string]any) (*StatusPage, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%q is not a status page id: %w", id, err)
	}
	if len(body) == 0 {
		return c.GetStatusPage(ctx, id)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.UpdateStatusPageWithBodyWithResponse(ctx, parsed, nil, "application/json", bytesReader(raw))
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var page StatusPage
	if err := json.Unmarshal(resp.Body, &page); err != nil {
		return nil, fmt.Errorf("decoding the updated status page: %w", err)
	}
	return &page, nil
}

// SetStatusPageComponents replaces the WHOLE component set, in display
// order. It is a snapshot, not a diff: a component the array omits is
// removed, and an existing row keeps its per-component subscriptions only
// when its id travels with it.
func (c *Client) SetStatusPageComponents(ctx context.Context, id string, components []StatusPageComponent) (*StatusPage, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%q is not a status page id: %w", id, err)
	}
	if components == nil {
		components = []StatusPageComponent{}
	}
	raw, err := json.Marshal(map[string]any{"components": components})
	if err != nil {
		return nil, err
	}
	resp, err := c.api.SetStatusPageComponentsWithBodyWithResponse(ctx, parsed, nil, "application/json", bytesReader(raw))
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var page StatusPage
	if err := json.Unmarshal(resp.Body, &page); err != nil {
		return nil, fmt.Errorf("decoding the status page: %w", err)
	}
	return &page, nil
}

// DeleteStatusPage removes a page and everything published under it. One
// that is already gone is not an error.
func (c *Client) DeleteStatusPage(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%q is not a status page id: %w", id, err)
	}
	if _, err := c.api.DeleteStatusPageWithResponse(ctx, parsed, nil); err != nil {
		if hosttracker.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// StatusPageFilter narrows a status page listing. The list door takes no
// filters of its own, so Slug is matched as the pages are drained.
type StatusPageFilter struct {
	// Slug keeps only the page published at this address.
	Slug string
	// Limit caps how many rows are collected across pages. Zero means the
	// package default.
	Limit int
}

// ListStatusPages walks the cursor pages and collects the rows. The list
// row carries no settings and no components; read a page by id for those.
func (c *Client) ListStatusPages(ctx context.Context, f StatusPageFilter) ([]StatusPage, error) {
	collected := f.Limit
	if collected <= 0 {
		collected = DefaultListCap
	}
	pageSize := int32(200)

	fetch := func(ctx context.Context, cursor *string) ([]StatusPage, *string, error) {
		resp, err := c.api.ListStatusPageWithResponse(ctx, &hosttracker.ListStatusPageParams{
			Cursor: cursor,
			Limit:  &pageSize,
		})
		if err != nil {
			return nil, nil, err
		}
		var page struct {
			Data       []StatusPage `json:"data"`
			NextCursor *string      `json:"nextCursor"`
		}
		if err := json.Unmarshal(resp.Body, &page); err != nil {
			return nil, nil, fmt.Errorf("decoding the status page listing: %w", err)
		}
		return page.Data, page.NextCursor, nil
	}

	out := make([]StatusPage, 0, 16)
	for page, err := range hosttracker.Paginate(ctx, fetch) {
		if err != nil {
			return nil, err
		}
		if f.Slug != "" && page.Slug != f.Slug {
			continue
		}
		out = append(out, page)
		if len(out) >= collected {
			break
		}
	}
	return out, nil
}
