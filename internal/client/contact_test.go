package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	contactFixtureID = "2c9a5f31-7b64-4e08-91d2-5a3c6e7b8f40"
	groupFixtureID   = "7d1b3e55-2a90-4c11-8f36-4b1d2e6a9c83"
)

func contactBody() map[string]any {
	return map[string]any{
		"id":            contactFixtureID,
		"type":          "email",
		"name":          "On-call",
		"address":       "oncall@example.com",
		"confirmed":     true,
		"overlimited":   false,
		"alertDelay":    5,
		"language":      "en",
		"groupedAlerts": true,
		"created":       1785670783,
		"updated":       1785670783,
		"activePeriod": map[string]any{
			"start":    "09:00:00",
			"end":      "18:00:00",
			"days":     []string{"Monday", "Friday"},
			"timezone": "W. Europe Standard Time",
		},
	}
}

func groupBody() map[string]any {
	return map[string]any{
		"id":      groupFixtureID,
		"name":    "Ops",
		"created": 1785670783,
		"items": []any{
			map[string]any{
				"contact": map[string]any{"id": contactFixtureID, "type": "email", "name": "On-call"},
				"events":  []string{"down", "up"},
			},
		},
	}
}

func TestGetContactDecodesTheStoredWindow(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("expand") != "template" {
			t.Errorf("expected the read to ask for templates, got %q", r.URL.RawQuery)
		}
		writeJSON(w, http.StatusOK, contactBody())
	})

	contact, err := api.GetContact(context.Background(), contactFixtureID)
	if err != nil {
		t.Fatalf("reading the contact: %v", err)
	}
	if contact.ActivePeriod == nil || contact.ActivePeriod.Start == nil || *contact.ActivePeriod.Start != "09:00:00" {
		t.Fatalf("expected the window to survive, got %#v", contact.ActivePeriod)
	}
	if contact.AlertDelay == nil || *contact.AlertDelay != 5 {
		t.Fatalf("expected an alert delay of 5 minutes, got %v", contact.AlertDelay)
	}
}

func TestGetContactReportsAGoneContact(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{"code": "not_found", "status": 404, "title": "Not found"})
	})

	if _, err := api.GetContact(context.Background(), contactFixtureID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateContactRereadsTheContact(t *testing.T) {
	var posted map[string]any
	reads := 0
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &posted)
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("expected the SDK to carry an idempotency key on a write")
			}
			created := contactBody()
			delete(created, "activePeriod")
			writeJSON(w, http.StatusCreated, created)
		default:
			reads++
			writeJSON(w, http.StatusOK, contactBody())
		}
	})

	contact, err := api.CreateContact(context.Background(), map[string]any{"type": "email", "address": "oncall@example.com"})
	if err != nil {
		t.Fatalf("creating the contact: %v", err)
	}
	if posted["address"] != "oncall@example.com" {
		t.Fatalf("expected the body to reach the API, got %v", posted)
	}
	if reads != 1 {
		t.Fatalf("expected exactly one follow-up read, got %d", reads)
	}
	if contact.ActivePeriod == nil {
		t.Fatal("expected the re-read to bring the window the create did not publish")
	}
}

func TestCreateContactAdoptsTheContactABindAnswersWith(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// 200 rather than 201: an equivalent contact already existed.
			writeJSON(w, http.StatusOK, contactBody())
			return
		}
		writeJSON(w, http.StatusOK, contactBody())
	})

	contact, err := api.CreateContact(context.Background(), map[string]any{"type": "email", "address": "oncall@example.com"})
	if err != nil {
		t.Fatalf("creating the contact: %v", err)
	}
	if contact.ID != contactFixtureID {
		t.Fatalf("expected the bound contact's id, got %q", contact.ID)
	}
}

func TestUpdateContactSendsNothingForAnEmptyDiff(t *testing.T) {
	patches := 0
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches++
		}
		writeJSON(w, http.StatusOK, contactBody())
	})

	if _, err := api.UpdateContact(context.Background(), contactFixtureID, map[string]any{}); err != nil {
		t.Fatalf("updating the contact: %v", err)
	}
	if patches != 0 {
		t.Fatalf("expected no PATCH for an empty body, got %d", patches)
	}
}

func TestDeleteContactTreatsAGoneContactAsDone(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{"code": "not_found", "status": 404, "title": "Not found"})
	})

	if err := api.DeleteContact(context.Background(), contactFixtureID); err != nil {
		t.Fatalf("expected a gone contact to be a successful delete, got %v", err)
	}
}

func TestSendContactConfirmationAcceptsAnAlreadyConfirmedContact(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusConflict, map[string]any{
			"code": "contact_already_confirmed", "status": 409, "title": "Already confirmed",
			"detail": "This contact has already confirmed its address.",
		})
	})

	if err := api.SendContactConfirmation(context.Background(), contactFixtureID); err != nil {
		t.Fatalf("expected an already-confirmed contact to need no code, got %v", err)
	}
}

func TestSendContactConfirmationReportsARefusal(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusTooManyRequests, map[string]any{
			"code": "rate_limited", "status": 429, "title": "Too many requests",
		})
	})

	err := api.SendContactConfirmation(context.Background(), contactFixtureID)
	if err == nil {
		t.Fatal("expected a throttled resend to be reported")
	}
	if diags := Diagnose("send the confirmation code", err, nil); len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
}

func TestListContactsKeepsTheExactAddress(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		other := contactBody()
		other["id"] = groupFixtureID
		other["address"] = "oncall@example.com.au"
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []any{contactBody(), other}, "nextCursor": nil,
		})
	})

	contacts, err := api.ListContacts(context.Background(), ContactFilter{Address: "oncall@example.com"})
	if err != nil {
		t.Fatalf("listing the contacts: %v", err)
	}
	if len(contacts) != 1 || contacts[0].ID != contactFixtureID {
		t.Fatalf("expected only the exact address to match, got %d rows", len(contacts))
	}
}

func TestListContactsSendsTheFilters(t *testing.T) {
	var query string
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}, "nextCursor": nil})
	})

	confirmed := false
	if _, err := api.ListContacts(context.Background(), ContactFilter{
		Type: []string{"sms", "voiceCall"}, Confirmed: &confirmed, Q: "ops",
	}); err != nil {
		t.Fatalf("listing the contacts: %v", err)
	}
	for _, want := range []string{"type=sms", "type=voiceCall", "confirmed=false", "q=ops"} {
		if !strings.Contains(query, want) {
			t.Fatalf("expected %q in the query, got %q", want, query)
		}
	}
}

func TestGetContactGroupReadsTheMembersIdentity(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, groupBody())
	})

	group, err := api.GetContactGroup(context.Background(), groupFixtureID)
	if err != nil {
		t.Fatalf("reading the group: %v", err)
	}
	if len(group.Items) != 1 {
		t.Fatalf("expected one member, got %d", len(group.Items))
	}
	if id := group.Items[0].ContactID(); id != contactFixtureID {
		t.Fatalf("expected the member's id out of its projection, got %q", id)
	}
}

func TestContactGroupItemReadsABareId(t *testing.T) {
	// A write carries the id itself where a read carries the projection.
	item := ContactGroupItem{Contact: json.RawMessage(`"` + contactFixtureID + `"`)}
	if id := item.ContactID(); id != contactFixtureID {
		t.Fatalf("expected the bare id to be readable, got %q", id)
	}
}

func TestCreateContactGroupRereadsTheGroup(t *testing.T) {
	reads := 0
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			created := groupBody()
			delete(created, "items")
			writeJSON(w, http.StatusCreated, created)
			return
		}
		reads++
		writeJSON(w, http.StatusOK, groupBody())
	})

	group, err := api.CreateContactGroup(context.Background(), map[string]any{"name": "Ops", "items": []any{}})
	if err != nil {
		t.Fatalf("creating the group: %v", err)
	}
	if reads != 1 || len(group.Items) != 1 {
		t.Fatalf("expected the re-read to bring the membership, got %d reads and %d members", reads, len(group.Items))
	}
}

func TestListContactGroupsWalksThePages(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			writeJSON(w, http.StatusOK, map[string]any{"data": []any{groupBody()}, "nextCursor": "page-2", "hasMore": true})
			return
		}
		second := groupBody()
		second["id"] = contactFixtureID
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{second}, "nextCursor": nil, "hasMore": false})
	})

	groups, err := api.ListContactGroups(context.Background(), 0)
	if err != nil {
		t.Fatalf("listing the groups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected both pages, got %d rows", len(groups))
	}
}

func TestDiagnoseExplainsAnUncreatableContactType(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, 422, map[string]any{
			"code": "contact_type_not_creatable", "status": 422, "title": "Type not creatable",
			"detail": "A telegram contact is created by registering with the bot.",
			"errors": []any{map[string]any{"pointer": "/type", "successor": "telegram bot registration"}},
		})
	})

	_, err := api.CreateContact(context.Background(), map[string]any{"type": "telegram"})
	diags := Diagnose("create the contact", err, nil)
	if len(diags) != 1 || !strings.Contains(diags[0].Detail(), "registering with the bot") {
		t.Fatalf("expected the API's own explanation, got %v", diags)
	}
}
