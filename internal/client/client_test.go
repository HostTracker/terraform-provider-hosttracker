package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
)

const testMonitorID = "8e2d4c8b-7a41-4a2b-9d0e-2f3a5c6b7d8e"

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	api, err := New(Config{Token: "test-token", BaseURL: server.URL, RetryMax: 0})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	return api
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeProblem(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Request-Id", "req_123")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func monitorBody() map[string]any {
	return map[string]any{
		"id":       testMonitorID,
		"type":     "http",
		"url":      "https://example.com",
		"name":     "Marketing site",
		"enabled":  true,
		"since":    1735689600,
		"updated":  1735689600,
		"created":  1735689500,
		"openStat": false,
		"fullLog":  false,
		"interval": 300,
		"tags":     []string{"prod"},
		"settings": map[string]any{"keywords": "Sign in", "followRedirect": true},
	}
}

func TestGetMonitorDecodesSettingsAsAMap(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("expand") != "settings" {
			t.Errorf("expected the read to ask for settings, got %q", r.URL.RawQuery)
		}
		writeJSON(w, http.StatusOK, monitorBody())
	})

	monitor, err := api.GetMonitor(context.Background(), testMonitorID)
	if err != nil {
		t.Fatalf("reading the monitor: %v", err)
	}
	if monitor.Settings["keywords"] != "Sign in" {
		t.Fatalf("expected the settings to survive, got %v", monitor.Settings)
	}
	if monitor.Interval == nil || *monitor.Interval != 300 {
		t.Fatalf("expected interval 300, got %v", monitor.Interval)
	}
}

func TestGetMonitorReportsAGoneMonitor(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{
			"type": "https://api2.host-tracker.com/problems/not-found", "code": "not_found",
			"status": 404, "title": "Not found", "detail": "No such monitor.",
		})
	})

	if _, err := api.GetMonitor(context.Background(), testMonitorID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateMonitorRereadsTheMonitor(t *testing.T) {
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
			created := monitorBody()
			delete(created, "effectiveUrl")
			writeJSON(w, http.StatusCreated, created)
		default:
			reads++
			body := monitorBody()
			body["effectiveUrl"] = "https://example.com/"
			writeJSON(w, http.StatusOK, body)
		}
	})

	monitor, err := api.CreateMonitor(context.Background(), map[string]any{"type": "http", "url": "https://example.com"})
	if err != nil {
		t.Fatalf("creating the monitor: %v", err)
	}
	if posted["url"] != "https://example.com" {
		t.Fatalf("expected the body to reach the API, got %v", posted)
	}
	if reads != 1 {
		t.Fatalf("expected exactly one follow-up read, got %d", reads)
	}
	if monitor.EffectiveURL == nil || *monitor.EffectiveURL != "https://example.com/" {
		t.Fatalf("expected the re-read to bring the effective url, got %v", monitor.EffectiveURL)
	}
}

func TestUpdateMonitorSendsNothingForAnEmptyDiff(t *testing.T) {
	patches := 0
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches++
		}
		writeJSON(w, http.StatusOK, monitorBody())
	})

	if _, err := api.UpdateMonitor(context.Background(), testMonitorID, map[string]any{}); err != nil {
		t.Fatalf("updating the monitor: %v", err)
	}
	if patches != 0 {
		t.Fatalf("expected no PATCH for an empty body, got %d", patches)
	}
}

func TestDeleteMonitorAcceptsAReceipt(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected a DELETE, got %s", r.Method)
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": testMonitorID, "deleted": true, "type": "http", "url": "https://example.com"})
	})

	if err := api.DeleteMonitor(context.Background(), testMonitorID); err != nil {
		t.Fatalf("deleting the monitor: %v", err)
	}
}

func TestDeleteMonitorTreatsAGoneMonitorAsDone(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{"code": "not_found", "status": 404, "title": "Not found"})
	})

	if err := api.DeleteMonitor(context.Background(), testMonitorID); err != nil {
		t.Fatalf("expected a gone monitor to be a successful delete, got %v", err)
	}
}

func TestListMonitorsWalksThePages(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			first := monitorBody()
			writeJSON(w, http.StatusOK, map[string]any{"data": []any{first}, "nextCursor": "page-2", "hasMore": true})
			return
		}
		second := monitorBody()
		second["id"] = "1c2d4c8b-7a41-4a2b-9d0e-2f3a5c6b7d8e"
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{second}, "nextCursor": nil, "hasMore": false})
	})

	monitors, err := api.ListMonitors(context.Background(), MonitorFilter{})
	if err != nil {
		t.Fatalf("listing the monitors: %v", err)
	}
	if len(monitors) != 2 {
		t.Fatalf("expected both pages, got %d rows", len(monitors))
	}
}

func TestListMonitorsStopsAtTheCap(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{monitorBody(), monitorBody(), monitorBody()}, "nextCursor": "more", "hasMore": true})
	})

	monitors, err := api.ListMonitors(context.Background(), MonitorFilter{Limit: 2})
	if err != nil {
		t.Fatalf("listing the monitors: %v", err)
	}
	if len(monitors) != 2 {
		t.Fatalf("expected the cap to hold at 2, got %d", len(monitors))
	}
}

func TestListMonitorsNormalizesTheTypeFilter(t *testing.T) {
	var seen string
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query().Get("type")
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}, "nextCursor": nil})
	})

	if _, err := api.ListMonitors(context.Background(), MonitorFilter{Type: []string{"pageSpeed"}}); err != nil {
		t.Fatalf("listing the monitors: %v", err)
	}
	if seen != "waterfall" {
		t.Fatalf("expected the alias to be resolved before it reached the API, got %q", seen)
	}
}

func TestDiagnoseAPlacesValidationFailuresOnAttributes(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, 422, map[string]any{
			"code": "invalid_interval", "status": 422, "title": "Invalid interval",
			"detail": "7 is not an interval this package sells.",
			"errors": []any{map[string]any{"pointer": "/interval", "value": 7, "allowed": []any{60, 300}}},
		})
	})

	_, err := api.GetMonitor(context.Background(), testMonitorID)
	mapper := func(pointer string) (path.Path, bool) {
		if pointer == "/interval" {
			return path.Root("interval"), true
		}
		return path.Empty(), false
	}

	diags := Diagnose("update the monitor", err, mapper)
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %d: %v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Detail(), "Allowed: 60, 300") {
		t.Fatalf("expected the allowed values in the detail, got %q", diags[0].Detail())
	}
	if !strings.Contains(diags[0].Detail(), "req_123") {
		t.Fatalf("expected the request id in the detail, got %q", diags[0].Detail())
	}
}

func TestDiagnoseExplainsAPackageLimit(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, 403, map[string]any{
			"code": "package_limit", "status": 403, "title": "Package limit",
			"detail": "This package does not sell the sslExp type.",
			"errors": []any{map[string]any{"feature": "sslPolicy"}},
		})
	})

	_, err := api.GetMonitor(context.Background(), testMonitorID)

	if feature, ok := PackageFeature(err); !ok || feature != "sslPolicy" {
		t.Fatalf("expected the feature to be readable, got %q (%v)", feature, ok)
	}
	diags := Diagnose("create the monitor", err, nil)
	if len(diags) != 1 || !strings.Contains(diags[0].Detail(), "sslPolicy") {
		t.Fatalf("expected the entitlement to be named, got %v", diags)
	}
	if diags[0].Summary() != "The account's package does not allow this" {
		t.Fatalf("unexpected summary %q", diags[0].Summary())
	}
}

func TestDiagnoseNamesTheExistingMonitorOnADuplicate(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, 409, map[string]any{
			"code": "duplicate_monitor", "status": 409, "title": "Duplicate monitor",
			"detail": "This address is already monitored.",
			"errors": []any{map[string]any{"pointer": "/url", "existingId": testMonitorID}},
		})
	})

	_, err := api.CreateMonitor(context.Background(), map[string]any{"type": "http", "url": "https://example.com"})

	existing, ok := ExistingID(err)
	if !ok || existing != testMonitorID {
		t.Fatalf("expected the existing id, got %q (%v)", existing, ok)
	}
	diags := Diagnose("create the monitor", err, nil)
	if len(diags) != 1 || !strings.Contains(diags[0].Detail(), "terraform import hosttracker_monitor.<name> "+testMonitorID) {
		t.Fatalf("expected an import instruction, got %v", diags)
	}
}

func TestDiagnoseExplainsAMissingScope(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, 403, map[string]any{
			"code": "missing_scope", "status": 403, "title": "Missing scope",
			"errors": []any{map[string]any{"required": "monitor:write", "granted": []any{"monitor:read"}}},
		})
	})

	_, err := api.GetMonitor(context.Background(), testMonitorID)
	diags := Diagnose("create the monitor", err, nil)
	if len(diags) != 1 || !strings.Contains(diags[0].Detail(), `"monitor:write"`) {
		t.Fatalf("expected the missing scope to be named, got %v", diags)
	}
}

func TestDiagnoseReportsAnUnreachableAPI(t *testing.T) {
	api, err := New(Config{Token: "t", BaseURL: "http://127.0.0.1:1", RetryMax: 0})
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.GetMonitor(context.Background(), testMonitorID)

	diags := Diagnose("read the monitor", err, nil)
	if len(diags) != 1 || !strings.Contains(diags[0].Detail(), "could not be reached") {
		t.Fatalf("expected an unreachable-API diagnostic, got %v", diags)
	}
}

func TestDiagnoseIsEmptyWithoutAnError(t *testing.T) {
	if diags := Diagnose("do nothing", nil, nil); len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
}
