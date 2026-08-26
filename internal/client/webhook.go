package client

import (
	"context"
	"encoding/json"
	"fmt"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
	"github.com/google/uuid"
)

// Webhook is the wire shape of a webhook endpoint.
type Webhook struct {
	ID                  string          `json:"id"`
	URL                 string          `json:"url,omitempty"`
	Events              []string        `json:"events,omitempty"`
	Scope               *WebhookScope   `json:"scope,omitempty"`
	Name                *string         `json:"name,omitempty"`
	Enabled             bool            `json:"enabled"`
	DisabledReason      *string         `json:"disabledReason,omitempty"`
	ConsecutiveFailures int64           `json:"consecutiveFailures,omitempty"`
	LastDeliveryAt      *int64          `json:"lastDeliveryAt,omitempty"`
	Headers             []WebhookHeader `json:"headers,omitempty"`
	Secret              *WebhookSecret  `json:"secret,omitempty"`
	Created             int64           `json:"created,omitempty"`
	Updated             int64           `json:"updated,omitempty"`
}

// WebhookScope says which monitors a webhook receives events for. A write
// carries exactly one of All, MonitorIDs or Tags; a read adds what the
// scope resolved to.
type WebhookScope struct {
	All                *bool    `json:"all,omitempty"`
	MonitorIDs         []string `json:"monitorIds,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	ResolvedMonitorIDs []string `json:"resolvedMonitorIds,omitempty"`
	MonitorCount       int64    `json:"monitorCount,omitempty"`
}

// WebhookHeader is one custom request header a delivery carries.
type WebhookHeader struct {
	Header string `json:"header"`
	Value  string `json:"value,omitempty"`
}

// WebhookSecret is the signing secret as a read publishes it. Value is
// present only in the answer that minted or rotated it.
type WebhookSecret struct {
	Set                bool   `json:"set"`
	UpdatedAt          *int64 `json:"updatedAt,omitempty"`
	Value              string `json:"value,omitempty"`
	PreviousValidUntil *int64 `json:"previousValidUntil,omitempty"`
}

// CreateWebhook posts a webhook and returns it as a read renders it, with
// the minted signing secret carried across.
//
// The read matters because the answer to a write can carry timestamps a
// second behind the row it wrote, and state that holds them would not match
// the same webhook adopted by an import. The carry matters because the create
// answer is the one and only time the secret's VALUE is published: a read
// publishes `{set: true}` and nothing more.
func (c *Client) CreateWebhook(ctx context.Context, body map[string]any) (*Webhook, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.CreateWebhookWithBodyWithResponse(ctx, nil, "application/json", bytesReader(raw))
	if err != nil {
		return nil, err
	}
	var created Webhook
	if err := json.Unmarshal(resp.Body, &created); err != nil {
		return nil, fmt.Errorf("decoding the created webhook: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("the create answered %d without a webhook id", statusOf(resp.HTTPResponse))
	}
	stored, err := c.GetWebhook(ctx, created.ID)
	if err != nil {
		return nil, err
	}
	carrySecretValue(stored, &created)
	return stored, nil
}

// carrySecretValue moves a freshly minted secret's value from the answer that
// published it onto the read that did not.
func carrySecretValue(into, from *Webhook) {
	if from.Secret == nil || from.Secret.Value == "" {
		return
	}
	if into.Secret == nil {
		into.Secret = &WebhookSecret{Set: true}
	}
	into.Secret.Value = from.Secret.Value
}

// GetWebhook reads one webhook. One that is gone answers ErrNotFound.
func (c *Client) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%q is not a webhook id: %w", id, err)
	}
	resp, err := c.api.GetWebhookWithResponse(ctx, parsed, nil)
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var w Webhook
	if err := json.Unmarshal(resp.Body, &w); err != nil {
		return nil, fmt.Errorf("decoding the webhook: %w", err)
	}
	return &w, nil
}

// UpdateWebhook sends a PATCH body and returns the answer. As with the
// create, the answer is used rather than a fresh read, because a rotation
// publishes the new secret exactly once. An empty body re-reads instead.
func (c *Client) UpdateWebhook(ctx context.Context, id string, body map[string]any) (*Webhook, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("%q is not a webhook id: %w", id, err)
	}
	if len(body) == 0 {
		return c.GetWebhook(ctx, id)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.UpdateWebhookWithBodyWithResponse(ctx, parsed, nil, "application/json", bytesReader(raw))
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var updated Webhook
	if err := json.Unmarshal(resp.Body, &updated); err != nil {
		return nil, fmt.Errorf("decoding the updated webhook: %w", err)
	}
	stored, err := c.GetWebhook(ctx, id)
	if err != nil {
		return nil, err
	}
	// A rotation publishes the new secret's value here and nowhere else.
	carrySecretValue(stored, &updated)
	return stored, nil
}

// DeleteWebhook removes a webhook. One that is already gone is not an error.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%q is not a webhook id: %w", id, err)
	}
	if _, err := c.api.DeleteWebhookWithResponse(ctx, parsed, nil); err != nil {
		if hosttracker.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// WebhookFilter narrows a webhook listing. The list door itself takes no
// filters, so URL is matched as the pages are drained.
type WebhookFilter struct {
	// URL keeps only webhooks POSTing to this exact address.
	URL string
	// Limit caps how many rows are collected across pages. Zero means the
	// package default.
	Limit int
}

// ListWebhooks walks the cursor pages and collects the rows.
func (c *Client) ListWebhooks(ctx context.Context, f WebhookFilter) ([]Webhook, error) {
	collected := f.Limit
	if collected <= 0 {
		collected = DefaultListCap
	}
	pageSize := int32(200)

	fetch := func(ctx context.Context, cursor *string) ([]Webhook, *string, error) {
		resp, err := c.api.ListWebhookWithResponse(ctx, &hosttracker.ListWebhookParams{
			Cursor: cursor,
			Limit:  &pageSize,
		})
		if err != nil {
			return nil, nil, err
		}
		var page struct {
			Data       []Webhook `json:"data"`
			NextCursor *string   `json:"nextCursor"`
		}
		if err := json.Unmarshal(resp.Body, &page); err != nil {
			return nil, nil, fmt.Errorf("decoding the webhook page: %w", err)
		}
		return page.Data, page.NextCursor, nil
	}

	out := make([]Webhook, 0, 16)
	for w, err := range hosttracker.Paginate(ctx, fetch) {
		if err != nil {
			return nil, err
		}
		if f.URL != "" && w.URL != f.URL {
			continue
		}
		out = append(out, w)
		if len(out) >= collected {
			break
		}
	}
	return out, nil
}
