package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"
)

const testContactID = "0c3c7b07-cecb-43dd-9b76-8516d3b9c771"

func TestSetAlertSubscriptionSendsTheWholeSet(t *testing.T) {
	var sent map[string]any
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected the write to PUT, got %s", r.Method)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatalf("decoding the request body: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"contact":    map[string]any{"id": testContactID, "type": "email"},
			"alertTypes": []string{"down", "up"},
			"created":    1785712681,
		})
	})

	sub, err := api.SetAlertSubscription(context.Background(), testMonitorID, testContactID, []string{"down", "up"})
	if err != nil {
		t.Fatalf("writing the subscription: %v", err)
	}

	want := []any{"down", "up"}
	if got, ok := sent["alertTypes"].([]any); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the whole set on the wire, got %v", sent)
	}
	if len(sent) != 1 {
		t.Fatalf("the body vocabulary is closed and holds one member, got %v", sent)
	}
	if !reflect.DeepEqual(sub.AlertTypes, []string{"down", "up"}) {
		t.Fatalf("expected the stored set back, got %v", sub.AlertTypes)
	}
	if sub.Created != 1785712681 {
		t.Fatalf("expected the stamp, got %d", sub.Created)
	}
}

func TestGetAlertSubscriptionTreatsAnEmptySetAsGone(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"created": 0})
	})

	if _, err := api.GetAlertSubscription(context.Background(), testMonitorID, testContactID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetAlertSubscriptionReportsAGonePair(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{
			"type": "https://api2.host-tracker.com/problems/not-found", "code": "not_found",
		})
	})

	if _, err := api.GetAlertSubscription(context.Background(), testMonitorID, testContactID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSetReportSubscriptionSendsFrequencies(t *testing.T) {
	var sent map[string]any
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatalf("decoding the request body: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"contact":     map[string]any{"id": testContactID, "type": "email"},
			"frequencies": []string{"monthly", "weekly"},
			"created":     1785712681,
		})
	})

	sub, err := api.SetReportSubscription(context.Background(), testMonitorID, testContactID,
		[]string{"monthly", "weekly"})
	if err != nil {
		t.Fatalf("writing the subscription: %v", err)
	}
	if _, ok := sent["frequencies"]; !ok {
		t.Fatalf("expected the frequencies on the wire, got %v", sent)
	}
	if !reflect.DeepEqual(sub.Frequencies, []string{"monthly", "weekly"}) {
		t.Fatalf("expected the stored set back, got %v", sub.Frequencies)
	}
}

func TestDeleteSubscriptionIgnoresAPairThatIsAlreadyGone(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{
			"type": "https://api2.host-tracker.com/problems/not-found", "code": "not_found",
		})
	})

	if err := api.DeleteAlertSubscription(context.Background(), testMonitorID, testContactID); err != nil {
		t.Fatalf("expected a gone pair to be no error, got %v", err)
	}
	if err := api.DeleteReportSubscription(context.Background(), testMonitorID, testContactID); err != nil {
		t.Fatalf("expected a gone pair to be no error, got %v", err)
	}
}

func TestSubscriptionRefusesAnIDThatIsNotOne(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a malformed id must never reach the API")
	})

	if _, err := api.GetAlertSubscription(context.Background(), "not-a-uuid", testContactID); err == nil {
		t.Fatal("expected a malformed monitor id to be refused")
	}
	if _, err := api.GetReportSubscription(context.Background(), testMonitorID, "not-a-uuid"); err == nil {
		t.Fatal("expected a malformed contact id to be refused")
	}
}
