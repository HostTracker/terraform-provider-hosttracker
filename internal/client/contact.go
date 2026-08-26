package client

import (
	"context"
	"encoding/json"
	"fmt"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
)

// Contact reads go through the SDK's untyped door rather than its
// generated views. The shapes below decode exactly the members this
// provider models, so a member a newer API publishes stays harmless.
// (SDK releases before v0.2.0 additionally could not decode a contact
// holding an active period at all; the typed views work since v0.2.0,
// but the local shapes keep the read surface deliberately small.)

// Contact is the wire shape of a delivery contact. The members that a
// write may clear are pointers, so that "absent" and "explicitly null"
// stay distinguishable on the way back in.
type Contact struct {
	ID                   string               `json:"id"`
	Type                 string               `json:"type,omitempty"`
	Name                 *string              `json:"name,omitempty"`
	Address              *string              `json:"address,omitempty"`
	Confirmed            bool                 `json:"confirmed"`
	Overlimited          bool                 `json:"overlimited"`
	AlertDelay           *int64               `json:"alertDelay,omitempty"`
	SendCost             *float64             `json:"sendCost,omitempty"`
	Gateway              *string              `json:"gateway,omitempty"`
	Language             *string              `json:"language,omitempty"`
	GroupedAlerts        bool                 `json:"groupedAlerts"`
	BillingNotifications *bool                `json:"billingNotifications,omitempty"`
	SendNews             *bool                `json:"sendNews,omitempty"`
	MimeType             *string              `json:"mimeType,omitempty"`
	HTTPHeaders          *[]ContactHeader     `json:"httpHeaders,omitempty"`
	Templates            *[]ContactTemplate   `json:"templates,omitempty"`
	ActivePeriod         *ContactActivePeriod `json:"activePeriod,omitempty"`
	BotID                *string              `json:"botId,omitempty"`
	Created              int64                `json:"created,omitempty"`
	Updated              int64                `json:"updated,omitempty"`
}

// ContactHeader is one header an http contact sends with every delivery.
type ContactHeader struct {
	Header string  `json:"header"`
	Value  *string `json:"value,omitempty"`
}

// ContactTemplate is one per-event message body.
type ContactTemplate struct {
	Event   string `json:"event"`
	Content string `json:"content,omitempty"`
}

// ContactActivePeriod is the daily window during which a contact accepts
// delivery.
type ContactActivePeriod struct {
	Start    *string  `json:"start,omitempty"`
	End      *string  `json:"end,omitempty"`
	Days     []string `json:"days,omitempty"`
	Timezone *string  `json:"timezone,omitempty"`
}

// ContactGroup is a passive preset: a named set of contacts, each with the
// events the preset holds for it.
type ContactGroup struct {
	ID      string             `json:"id"`
	Name    *string            `json:"name,omitempty"`
	Created int64              `json:"created,omitempty"`
	Items   []ContactGroupItem `json:"items,omitempty"`
}

// ContactGroupItem is one member of a group. A write carries the contact's
// id; a read carries its identifying projection under the same member.
type ContactGroupItem struct {
	Contact json.RawMessage `json:"contact,omitempty"`
	Events  []string        `json:"events,omitempty"`
}

// ContactID reads the member's contact id out of either shape.
func (i ContactGroupItem) ContactID() string {
	if len(i.Contact) == 0 {
		return ""
	}
	var id string
	if err := json.Unmarshal(i.Contact, &id); err == nil {
		return id
	}
	var ref struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(i.Contact, &ref); err == nil {
		return ref.ID
	}
	return ""
}

// CreateContact posts a contact and returns it as a read renders it.
//
// The create is a bind as much as a create: when the account already holds
// a contact with the same type, address, gateway and alert delay, the API
// answers 200 with that contact instead of writing a second one. The id
// that comes back is therefore the id to manage either way.
func (c *Client) CreateContact(ctx context.Context, body map[string]any) (*Contact, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.CreateContactWithBody(ctx, nil, "application/json", bytesReader(raw))
	if err != nil {
		return nil, err
	}
	var created Contact
	if err := decodeBody(resp, &created); err != nil {
		return nil, fmt.Errorf("decoding the created contact: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("the create answered %d without a contact id", statusOf(resp))
	}
	return c.GetContact(ctx, created.ID)
}

// GetContact reads one contact, with the custom templates a read only
// carries when it is asked for them. A contact that is gone answers
// ErrNotFound.
func (c *Client) GetContact(ctx context.Context, id string) (*Contact, error) {
	parsed, err := parseID(id, "contact")
	if err != nil {
		return nil, err
	}
	expand := []hosttracker.GetContactParamsExpand{"template"}
	resp, err := c.api.GetContact(ctx, parsed, &hosttracker.GetContactParams{Expand: &expand})
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var contact Contact
	if err := decodeBody(resp, &contact); err != nil {
		return nil, fmt.Errorf("decoding the contact: %w", err)
	}
	return &contact, nil
}

// UpdateContact sends a PATCH body and returns the contact as a read
// renders it. An empty body is a no-op that still re-reads.
func (c *Client) UpdateContact(ctx context.Context, id string, body map[string]any) (*Contact, error) {
	parsed, err := parseID(id, "contact")
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		resp, err := c.api.UpdateContactWithBody(ctx, parsed, nil, "application/json", bytesReader(raw))
		if err != nil {
			if hosttracker.IsNotFound(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		drain(resp)
	}
	return c.GetContact(ctx, id)
}

// DeleteContact removes a contact. One that is already gone is not an
// error.
func (c *Client) DeleteContact(ctx context.Context, id string) error {
	parsed, err := parseID(id, "contact")
	if err != nil {
		return err
	}
	if _, err := c.api.DeleteContactWithResponse(ctx, parsed, nil); err != nil {
		if hosttracker.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// SendContactConfirmation asks the API to send the contact's confirmation
// code again. A contact that is already confirmed needs no code, so that
// refusal counts as success.
func (c *Client) SendContactConfirmation(ctx context.Context, id string) error {
	parsed, err := parseID(id, "contact")
	if err != nil {
		return err
	}
	if _, err := c.api.ResendContactConfirmationWithResponse(ctx, parsed, nil); err != nil {
		if hosttracker.IsCode(err, "contact_already_confirmed") {
			return nil
		}
		return err
	}
	return nil
}

// ContactFilter narrows a contact listing. Every member is optional and
// they are ANDed.
type ContactFilter struct {
	Type      []string
	Confirmed *bool
	Q         string
	// Address and Name keep only the rows matching exactly, which the API
	// itself does not offer: its `q` is a substring over both.
	Address string
	Name    string
	// Limit caps how many rows are collected across pages. Zero means the
	// package default.
	Limit int
}

// ListContacts walks the cursor pages and collects the rows, stopping at
// the filter's cap.
func (c *Client) ListContacts(ctx context.Context, f ContactFilter) ([]Contact, error) {
	collected := f.Limit
	if collected <= 0 {
		collected = DefaultListCap
	}
	pageSize := int32(200)
	if collected < int(pageSize) {
		pageSize = int32(collected)
	}

	fetch := func(ctx context.Context, cursor *string) ([]Contact, *string, error) {
		params := &hosttracker.ListContactParams{Cursor: cursor, Limit: &pageSize}
		if len(f.Type) > 0 {
			v := make([]hosttracker.ListContactParamsType, 0, len(f.Type))
			for _, t := range f.Type {
				v = append(v, hosttracker.ListContactParamsType(t))
			}
			params.Type = &v
		}
		if f.Confirmed != nil {
			params.Confirmed = f.Confirmed
		}
		if f.Q != "" {
			params.Q = &f.Q
		}
		resp, err := c.api.ListContact(ctx, params)
		if err != nil {
			return nil, nil, err
		}
		var page struct {
			Data       []Contact `json:"data"`
			NextCursor *string   `json:"nextCursor"`
		}
		if err := decodeBody(resp, &page); err != nil {
			return nil, nil, fmt.Errorf("decoding the contact page: %w", err)
		}
		return page.Data, page.NextCursor, nil
	}

	out := make([]Contact, 0, 16)
	for contact, err := range hosttracker.Paginate(ctx, fetch) {
		if err != nil {
			return nil, err
		}
		if f.Address != "" && (contact.Address == nil || *contact.Address != f.Address) {
			continue
		}
		if f.Name != "" && (contact.Name == nil || *contact.Name != f.Name) {
			continue
		}
		out = append(out, contact)
		if len(out) >= collected {
			break
		}
	}
	return out, nil
}

// ListContactTypes reads the contact-type catalogue, which is reference
// data and needs no token.
func (c *Client) ListContactTypes(ctx context.Context) ([]hosttracker.ContactTypeRow, error) {
	limit := int32(200)
	fetch := func(ctx context.Context, cursor *string) ([]hosttracker.ContactTypeRow, *string, error) {
		page, err := c.api.ListContactTypeWithResponse(ctx, &hosttracker.ListContactTypeParams{Cursor: cursor, Limit: &limit})
		if err != nil {
			return nil, nil, err
		}
		if page.JSON200 == nil {
			return nil, nil, nil
		}
		return page.JSON200.Data, page.JSON200.NextCursor, nil
	}
	return hosttracker.Collect(ctx, fetch, 0)
}

// CreateContactGroup posts a group and returns it as a read renders it.
func (c *Client) CreateContactGroup(ctx context.Context, body map[string]any) (*ContactGroup, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.CreateContactGroupWithBodyWithResponse(ctx, nil, "application/json", bytesReader(raw))
	if err != nil {
		return nil, err
	}
	var created ContactGroup
	if err := json.Unmarshal(resp.Body, &created); err != nil {
		return nil, fmt.Errorf("decoding the created contact group: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("the create answered %d without a group id", statusOf(resp.HTTPResponse))
	}
	return c.GetContactGroup(ctx, created.ID)
}

// GetContactGroup reads one group with its membership.
func (c *Client) GetContactGroup(ctx context.Context, id string) (*ContactGroup, error) {
	parsed, err := parseID(id, "contact group")
	if err != nil {
		return nil, err
	}
	resp, err := c.api.GetContactGroupWithResponse(ctx, parsed, &hosttracker.GetContactGroupParams{})
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var group ContactGroup
	if err := json.Unmarshal(resp.Body, &group); err != nil {
		return nil, fmt.Errorf("decoding the contact group: %w", err)
	}
	return &group, nil
}

// UpdateContactGroup sends a PATCH body and re-reads the group.
func (c *Client) UpdateContactGroup(ctx context.Context, id string, body map[string]any) (*ContactGroup, error) {
	parsed, err := parseID(id, "contact group")
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		if _, err := c.api.UpdateContactGroupWithBodyWithResponse(ctx, parsed, nil, "application/json", bytesReader(raw)); err != nil {
			if hosttracker.IsNotFound(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	return c.GetContactGroup(ctx, id)
}

// DeleteContactGroup removes a group. The contacts in it are untouched:
// a group is a preset, not an owner.
func (c *Client) DeleteContactGroup(ctx context.Context, id string) error {
	parsed, err := parseID(id, "contact group")
	if err != nil {
		return err
	}
	if _, err := c.api.DeleteContactGroupWithResponse(ctx, parsed, nil); err != nil {
		if hosttracker.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ListContactGroups walks the cursor pages and collects the groups.
func (c *Client) ListContactGroups(ctx context.Context, limit int) ([]ContactGroup, error) {
	collected := limit
	if collected <= 0 {
		collected = DefaultListCap
	}
	pageSize := int32(200)
	if collected < int(pageSize) {
		pageSize = int32(collected)
	}

	fetch := func(ctx context.Context, cursor *string) ([]ContactGroup, *string, error) {
		resp, err := c.api.ListContactGroupWithResponse(ctx, &hosttracker.ListContactGroupParams{Cursor: cursor, Limit: &pageSize})
		if err != nil {
			return nil, nil, err
		}
		var page struct {
			Data       []ContactGroup `json:"data"`
			NextCursor *string        `json:"nextCursor"`
		}
		if err := json.Unmarshal(resp.Body, &page); err != nil {
			return nil, nil, fmt.Errorf("decoding the contact-group page: %w", err)
		}
		return page.Data, page.NextCursor, nil
	}
	return hosttracker.Collect(ctx, fetch, collected)
}
