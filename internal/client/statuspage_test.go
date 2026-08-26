package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

const testStatusPageID = "6b1f2a70-3c8e-4d51-9f2a-7c4e5d6b8a90"

func statusPageBody() map[string]any {
	return map[string]any{
		"id":                  testStatusPageID,
		"slug":                "acme-status",
		"title":               "Acme status",
		"componentCount":      2,
		"unresolvedIncidents": 0,
		"hasPassword":         false,
		"created":             1785670783,
		"settings": map[string]any{
			"theme":     "light",
			"language":  "en",
			"features":  []string{"barCharts", "subscribe"},
			"slaTarget": 99.9,
		},
		"components": []map[string]any{
			{
				"id":         "9d3c1e64-5a72-4b18-8e0d-3f6a2c7b4d55",
				"monitorId":  testMonitorID,
				"name":       "Marketing site",
				"group":      "Public",
				"thirdParty": false,
			},
			{
				"id":          "1a2b3c4d-5e6f-4a1b-8c9d-0e1f2a3b4c5d",
				"name":        "Payments provider",
				"thirdParty":  true,
				"manualState": "operational",
			},
		},
	}
}

func TestGetStatusPageDecodesSettingsAsAMapAndComponentsInOrder(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, statusPageBody())
	})

	page, err := api.GetStatusPage(context.Background(), testStatusPageID)
	if err != nil {
		t.Fatalf("reading the status page: %v", err)
	}
	if page.Settings["theme"] != "light" {
		t.Fatalf("expected the settings to survive, got %v", page.Settings)
	}
	if len(page.Components) != 2 {
		t.Fatalf("expected two components, got %d", len(page.Components))
	}
	if page.Components[0].MonitorID == nil || *page.Components[0].MonitorID != testMonitorID {
		t.Fatalf("expected the monitored component first, got %+v", page.Components[0])
	}
	if !page.Components[1].ThirdParty {
		t.Fatalf("expected the third-party component second, got %+v", page.Components[1])
	}
}

func TestCreateStatusPageCarriesTheInitialComponents(t *testing.T) {
	var sent map[string]any
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected the create to POST, got %s", r.Method)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatalf("decoding the request body: %v", err)
		}
		writeJSON(w, http.StatusOK, statusPageBody())
	})

	page, err := api.CreateStatusPage(context.Background(), map[string]any{
		"slug":  "acme-status",
		"title": "Acme status",
		"components": []StatusPageComponent{
			{MonitorID: stringOf(testMonitorID)},
		},
	})
	if err != nil {
		t.Fatalf("creating the status page: %v", err)
	}
	components, ok := sent["components"].([]any)
	if !ok || len(components) != 1 {
		t.Fatalf("expected the component set inline, got %v", sent["components"])
	}
	if page.ID != testStatusPageID {
		t.Fatalf("expected the created page, got %q", page.ID)
	}
}

func TestSetStatusPageComponentsSendsTheWholeSetInOrder(t *testing.T) {
	var sent map[string]any
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected the component set to PUT, got %s", r.Method)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatalf("decoding the request body: %v", err)
		}
		writeJSON(w, http.StatusOK, statusPageBody())
	})

	rowID := "9d3c1e64-5a72-4b18-8e0d-3f6a2c7b4d55"
	if _, err := api.SetStatusPageComponents(context.Background(), testStatusPageID, []StatusPageComponent{
		{ID: stringOf(rowID), MonitorID: stringOf(testMonitorID)},
		{ThirdParty: true, Name: stringOf("Payments provider"), ManualState: stringOf("degraded")},
	}); err != nil {
		t.Fatalf("writing the components: %v", err)
	}

	components, ok := sent["components"].([]any)
	if !ok || len(components) != 2 {
		t.Fatalf("expected two components on the wire, got %v", sent["components"])
	}
	first, _ := components[0].(map[string]any)
	if first["id"] != rowID {
		t.Fatalf("expected the existing row's id to travel, got %v", first)
	}
	if _, present := first["thirdParty"]; present {
		t.Fatalf("a monitored component must not claim to be third-party, got %v", first)
	}
	second, _ := components[1].(map[string]any)
	if second["thirdParty"] != true || second["manualState"] != "degraded" {
		t.Fatalf("expected the pinned third-party component, got %v", second)
	}
}

func TestSetStatusPageComponentsClearsWithAnEmptySet(t *testing.T) {
	var sent map[string]any
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatalf("decoding the request body: %v", err)
		}
		writeJSON(w, http.StatusOK, statusPageBody())
	})

	if _, err := api.SetStatusPageComponents(context.Background(), testStatusPageID, nil); err != nil {
		t.Fatalf("clearing the components: %v", err)
	}
	components, ok := sent["components"].([]any)
	if !ok || len(components) != 0 {
		t.Fatalf("expected an empty array rather than a null, got %v", sent["components"])
	}
}

func TestStatusPagePublicURL(t *testing.T) {
	page := StatusPage{Slug: "acme-status"}
	if got := page.PublicURL(); got != "https://"+StatusPageHost+"/acme-status" {
		t.Fatalf("expected the shared origin, got %q", got)
	}

	page.CustomDomain = stringOf("status.acme.example")
	if got := page.PublicURL(); got != "https://status.acme.example/" {
		t.Fatalf("expected the custom domain to win, got %q", got)
	}
}

func TestListStatusPagesKeepsOnlyTheMatchingSlug(t *testing.T) {
	other := statusPageBody()
	other["id"] = "7c2e3b81-4d9f-4e62-af3b-8d5f6e7c9b01"
	other["slug"] = "other-status"

	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data":       []map[string]any{statusPageBody(), other},
			"nextCursor": nil,
		})
	})

	pages, err := api.ListStatusPages(context.Background(), StatusPageFilter{Slug: "other-status"})
	if err != nil {
		t.Fatalf("listing the status pages: %v", err)
	}
	if len(pages) != 1 || pages[0].Slug != "other-status" {
		t.Fatalf("expected only the matching page, got %+v", pages)
	}
}

func stringOf(v string) *string { return &v }
