package client

import (
	"context"
	"encoding/json"
	"fmt"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
	"github.com/google/uuid"
)

// SubscriptionContactRef is the identifying projection of the contact at
// the far end of a subscription.
type SubscriptionContactRef struct {
	ID      string  `json:"id,omitempty"`
	Type    *string `json:"type,omitempty"`
	Address *string `json:"address,omitempty"`
	Name    *string `json:"name,omitempty"`
}

// AlertSubscription is one monitor-and-contact alert pair, as the monitor
// side publishes it: a SET of alert types, not one row per type.
type AlertSubscription struct {
	Contact    *SubscriptionContactRef `json:"contact,omitempty"`
	AlertTypes []string                `json:"alertTypes,omitempty"`
	Created    int64                   `json:"created,omitempty"`
}

// ReportSubscription is one monitor-and-contact report pair: a SET of
// frequencies. Reports are delivered by email only.
type ReportSubscription struct {
	Contact     *SubscriptionContactRef `json:"contact,omitempty"`
	Frequencies []string                `json:"frequencies,omitempty"`
	Created     int64                   `json:"created,omitempty"`
}

func subscriptionIDs(monitorID, contactID string) (uuid.UUID, uuid.UUID, error) {
	monitor, err := uuid.Parse(monitorID)
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, fmt.Errorf("%q is not a monitor id: %w", monitorID, err)
	}
	contact, err := uuid.Parse(contactID)
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, fmt.Errorf("%q is not a contact id: %w", contactID, err)
	}
	return monitor, contact, nil
}

// GetAlertSubscription reads the alert pair. A pair that does not exist
// answers ErrNotFound.
func (c *Client) GetAlertSubscription(ctx context.Context, monitorID, contactID string) (*AlertSubscription, error) {
	monitor, contact, err := subscriptionIDs(monitorID, contactID)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.GetMonitorAlertWithResponse(ctx, monitor, contact, nil)
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var sub AlertSubscription
	if err := json.Unmarshal(resp.Body, &sub); err != nil {
		return nil, fmt.Errorf("decoding the alert subscription: %w", err)
	}
	// A pair with no alert types is a pair that is not there. The door
	// answers 404 for that, but a projection that kept nothing would leave
	// an empty body rather than an error.
	if len(sub.AlertTypes) == 0 {
		return nil, ErrNotFound
	}
	return &sub, nil
}

// SetAlertSubscription writes the EXACT set of alert types for the pair.
// The door is idempotent, so it serves as both create and update.
func (c *Client) SetAlertSubscription(ctx context.Context, monitorID, contactID string, alertTypes []string) (*AlertSubscription, error) {
	monitor, contact, err := subscriptionIDs(monitorID, contactID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{"alertTypes": alertTypes})
	if err != nil {
		return nil, err
	}
	resp, err := c.api.SetMonitorAlertWithBodyWithResponse(ctx, monitor, contact, nil, "application/json", bytesReader(raw))
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var sub AlertSubscription
	if err := json.Unmarshal(resp.Body, &sub); err != nil {
		return nil, fmt.Errorf("decoding the alert subscription: %w", err)
	}
	return &sub, nil
}

// DeleteAlertSubscription removes the pair. One that is already gone is
// not an error.
func (c *Client) DeleteAlertSubscription(ctx context.Context, monitorID, contactID string) error {
	monitor, contact, err := subscriptionIDs(monitorID, contactID)
	if err != nil {
		return err
	}
	if _, err := c.api.DeleteMonitorAlertWithResponse(ctx, monitor, contact, nil); err != nil {
		if hosttracker.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// GetReportSubscription reads the report pair. A pair that does not exist
// answers ErrNotFound.
func (c *Client) GetReportSubscription(ctx context.Context, monitorID, contactID string) (*ReportSubscription, error) {
	monitor, contact, err := subscriptionIDs(monitorID, contactID)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.GetMonitorReportWithResponse(ctx, monitor, contact, nil)
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var sub ReportSubscription
	if err := json.Unmarshal(resp.Body, &sub); err != nil {
		return nil, fmt.Errorf("decoding the report subscription: %w", err)
	}
	if len(sub.Frequencies) == 0 {
		return nil, ErrNotFound
	}
	return &sub, nil
}

// SetReportSubscription writes the EXACT set of frequencies for the pair.
func (c *Client) SetReportSubscription(ctx context.Context, monitorID, contactID string, frequencies []string) (*ReportSubscription, error) {
	monitor, contact, err := subscriptionIDs(monitorID, contactID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{"frequencies": frequencies})
	if err != nil {
		return nil, err
	}
	resp, err := c.api.SetMonitorReportWithBodyWithResponse(ctx, monitor, contact, nil, "application/json", bytesReader(raw))
	if err != nil {
		if hosttracker.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var sub ReportSubscription
	if err := json.Unmarshal(resp.Body, &sub); err != nil {
		return nil, fmt.Errorf("decoding the report subscription: %w", err)
	}
	return &sub, nil
}

// DeleteReportSubscription removes the pair. One that is already gone is
// not an error.
func (c *Client) DeleteReportSubscription(ctx context.Context, monitorID, contactID string) error {
	monitor, contact, err := subscriptionIDs(monitorID, contactID)
	if err != nil {
		return err
	}
	if _, err := c.api.DeleteMonitorReportWithResponse(ctx, monitor, contact, nil); err != nil {
		if hosttracker.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
