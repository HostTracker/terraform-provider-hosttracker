package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const windowFixtureID = "4e49d7a2-4ab5-45e2-b9f8-1d59f505ad45"

func windowBody() map[string]any {
	return map[string]any{
		"id":          windowFixtureID,
		"name":        "Weekly database maintenance",
		"from":        1785712608,
		"to":          1785716208,
		"durationSec": 3600,
		"timezone":    "Europe/Berlin",
		"enabled":     true,
		"state":       "scheduled",
		"overlimited": false,
		"recurrence":  map[string]any{"weekDays": []string{"Sunday"}},
		"monitorIds":  []string{testMonitorID},
		"monitors": []any{
			map[string]any{
				"monitorId": testMonitorID,
				"suppress":  map[string]any{"alerts": true, "stats": false},
			},
		},
		"suppress": map[string]any{"alerts": true, "stats": false},
		"created":  1785670783,
		"updated":  1785670783,
	}
}

func TestGetMaintenanceDecodesTheCoverage(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, windowBody())
	})

	window, err := api.GetMaintenance(context.Background(), windowFixtureID)
	if err != nil {
		t.Fatalf("reading the window: %v", err)
	}
	if window.DurationSec != 3600 || window.To != 1785716208 {
		t.Fatalf("expected both spellings of the length, got %d and %d", window.DurationSec, window.To)
	}
	if len(window.Monitors) != 1 || window.Monitors[0].Suppress == nil || !window.Monitors[0].Suppress.Alerts {
		t.Fatalf("expected the per-monitor suppression, got %#v", window.Monitors)
	}
	if window.Recurrence == nil || len(window.Recurrence.WeekDays) != 1 {
		t.Fatalf("expected the recurrence, got %#v", window.Recurrence)
	}
}

func TestCreateMaintenanceRereadsTheWindow(t *testing.T) {
	var posted map[string]any
	reads := 0
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &posted)
			created := windowBody()
			delete(created, "monitors")
			writeJSON(w, http.StatusCreated, created)
			return
		}
		reads++
		writeJSON(w, http.StatusOK, windowBody())
	})

	window, err := api.CreateMaintenance(context.Background(), map[string]any{
		"name": "Weekly database maintenance", "from": 1785712608, "durationSec": 3600,
	})
	if err != nil {
		t.Fatalf("creating the window: %v", err)
	}
	if posted["durationSec"] != float64(3600) {
		t.Fatalf("expected the length to reach the API, got %v", posted)
	}
	if reads != 1 || len(window.Monitors) != 1 {
		t.Fatalf("expected the re-read to bring the coverage, got %d reads and %d entries", reads, len(window.Monitors))
	}
}

func TestUpdateMaintenanceSendsOnlyTheBody(t *testing.T) {
	var patched map[string]any
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &patched)
		}
		writeJSON(w, http.StatusOK, windowBody())
	})

	if _, err := api.UpdateMaintenance(context.Background(), windowFixtureID, map[string]any{"enabled": false}); err != nil {
		t.Fatalf("updating the window: %v", err)
	}
	if len(patched) != 1 || patched["enabled"] != false {
		t.Fatalf("expected only the changed member on the wire, got %v", patched)
	}
}

func TestDeleteMaintenanceTreatsAGoneWindowAsDone(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{"code": "not_found", "status": 404, "title": "Not found"})
	})

	if err := api.DeleteMaintenance(context.Background(), windowFixtureID); err != nil {
		t.Fatalf("expected a gone window to be a successful delete, got %v", err)
	}
}

func TestListMaintenancesSendsTheFilters(t *testing.T) {
	var query string
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}, "nextCursor": nil})
	})

	if _, err := api.ListMaintenances(context.Background(), MaintenanceFilter{
		From: 100, To: 200, State: []string{"scheduled", "active"}, Monitor: []string{testMonitorID},
	}); err != nil {
		t.Fatalf("listing the windows: %v", err)
	}
	for _, want := range []string{"from=100", "to=200", "state=scheduled", "state=active", "monitor=" + testMonitorID} {
		if !strings.Contains(query, want) {
			t.Fatalf("expected %q in the query, got %q", want, query)
		}
	}
}

func TestListMaintenancesKeepsTheExactName(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		other := windowBody()
		other["id"] = contactFixtureID
		other["name"] = "Something else"
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{windowBody(), other}, "nextCursor": nil})
	})

	windows, err := api.ListMaintenances(context.Background(), MaintenanceFilter{Name: "Weekly database maintenance"})
	if err != nil {
		t.Fatalf("listing the windows: %v", err)
	}
	if len(windows) != 1 || windows[0].ID != windowFixtureID {
		t.Fatalf("expected only the named window, got %d rows", len(windows))
	}
}
