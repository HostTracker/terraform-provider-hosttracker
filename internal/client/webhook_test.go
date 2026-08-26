package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

const testWebhookID = "0c3c7b07-cecb-43dd-9b76-8516d3b9c771"

func webhookBody(secret map[string]any) map[string]any {
	body := map[string]any{
		"id":      testWebhookID,
		"url":     "https://hooks.example.com/host-tracker",
		"events":  []string{"monitor.down", "monitor.up"},
		"name":    "Ops channel",
		"enabled": true,
		"scope": map[string]any{
			"all":          true,
			"monitorCount": 8,
		},
		"consecutiveFailures": 0,
		"created":             1785712681,
		"updated":             1785712680,
	}
	if secret != nil {
		body["secret"] = secret
	}
	return body
}

func TestCreateWebhookReadsBackAndKeepsTheMintedSecret(t *testing.T) {
	calls := 0
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.Method {
		case http.MethodPost:
			writeJSON(w, http.StatusOK, webhookBody(map[string]any{
				"set":       true,
				"updatedAt": 1785712680,
				"value":     "whsec_the-one-and-only-reveal",
			}))
		case http.MethodGet:
			// What a read publishes: the secret is set, and that is all.
			writeJSON(w, http.StatusOK, webhookBody(map[string]any{
				"set":       true,
				"updatedAt": 1785712680,
			}))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	webhook, err := api.CreateWebhook(context.Background(), map[string]any{
		"url":    "https://hooks.example.com/host-tracker",
		"events": []string{"monitor.down", "monitor.up"},
		"scope":  map[string]any{"all": true},
	})
	if err != nil {
		t.Fatalf("creating the webhook: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected the write and a read back, got %d calls", calls)
	}
	if webhook.Secret == nil || webhook.Secret.Value != "whsec_the-one-and-only-reveal" {
		t.Fatalf("expected the minted secret to survive, got %+v", webhook.Secret)
	}
	if webhook.Scope == nil || webhook.Scope.All == nil || !*webhook.Scope.All {
		t.Fatalf("expected an all scope, got %+v", webhook.Scope)
	}
	if webhook.Scope.MonitorCount != 8 {
		t.Fatalf("expected the resolved count, got %d", webhook.Scope.MonitorCount)
	}
}

func TestUpdateWebhookRotatesAndAnswersTheNewSecret(t *testing.T) {
	var sent map[string]any
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &sent); err != nil {
				t.Fatalf("decoding the request body: %v", err)
			}
			writeJSON(w, http.StatusOK, webhookBody(map[string]any{
				"set":                true,
				"updatedAt":          1785799080,
				"value":              "whsec_the-new-one",
				"previousValidUntil": 1785885480,
			}))
		case http.MethodGet:
			writeJSON(w, http.StatusOK, webhookBody(map[string]any{
				"set":                true,
				"updatedAt":          1785799080,
				"previousValidUntil": 1785885480,
			}))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	webhook, err := api.UpdateWebhook(context.Background(), testWebhookID, map[string]any{
		"secret": map[string]any{"rotate": true},
	})
	if err != nil {
		t.Fatalf("rotating the secret: %v", err)
	}

	secret, ok := sent["secret"].(map[string]any)
	if !ok || secret["rotate"] != true {
		t.Fatalf("expected the body to ask for a rotation, got %v", sent)
	}
	if webhook.Secret.Value != "whsec_the-new-one" {
		t.Fatalf("expected the new secret, got %q", webhook.Secret.Value)
	}
	if webhook.Secret.PreviousValidUntil == nil || *webhook.Secret.PreviousValidUntil != 1785885480 {
		t.Fatalf("expected the grace window, got %v", webhook.Secret.PreviousValidUntil)
	}
}

func TestUpdateWebhookWithAnEmptyBodyReadsInstead(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected an empty body to read, got %s", r.Method)
		}
		writeJSON(w, http.StatusOK, webhookBody(map[string]any{"set": true}))
	})

	webhook, err := api.UpdateWebhook(context.Background(), testWebhookID, map[string]any{})
	if err != nil {
		t.Fatalf("reading the webhook: %v", err)
	}
	if webhook.ID != testWebhookID {
		t.Fatalf("expected the webhook back, got %q", webhook.ID)
	}
}

func TestGetWebhookReportsAGoneWebhook(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{
			"type": "https://api2.host-tracker.com/problems/not-found", "code": "not_found",
		})
	})

	if _, err := api.GetWebhook(context.Background(), testWebhookID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteWebhookIgnoresOneThatIsAlreadyGone(t *testing.T) {
	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, map[string]any{
			"type": "https://api2.host-tracker.com/problems/not-found", "code": "not_found",
		})
	})

	if err := api.DeleteWebhook(context.Background(), testWebhookID); err != nil {
		t.Fatalf("expected a gone webhook to be no error, got %v", err)
	}
}

func TestListWebhooksKeepsOnlyTheMatchingAddress(t *testing.T) {
	other := webhookBody(nil)
	other["id"] = "2c6a1f1e-6f0e-4a2e-9c56-1d59f505ad45"
	other["url"] = "https://elsewhere.example.com/hook"

	api := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data":       []map[string]any{webhookBody(nil), other},
			"nextCursor": nil,
		})
	})

	webhooks, err := api.ListWebhooks(context.Background(), WebhookFilter{
		URL: "https://hooks.example.com/host-tracker",
	})
	if err != nil {
		t.Fatalf("listing the webhooks: %v", err)
	}
	if len(webhooks) != 1 || webhooks[0].ID != testWebhookID {
		t.Fatalf("expected only the matching webhook, got %+v", webhooks)
	}
}
