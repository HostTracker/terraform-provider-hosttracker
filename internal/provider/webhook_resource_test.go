package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	testWebhookID   = "0c3c7b07-cecb-43dd-9b76-8516d3b9c771"
	testWebhookAddr = "https://hooks.example.com/host-tracker"
)

func TestSubscriptionAndDeliverySchemasAreValid(t *testing.T) {
	ctx := context.Background()
	for _, build := range []func() fwresource.Resource{
		NewWebhookResource,
		NewAlertSubscriptionResource,
		NewReportSubscriptionResource,
		NewStatusPageResource,
	} {
		resp := &fwresource.SchemaResponse{}
		build().Schema(ctx, fwresource.SchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("building the schema: %v", resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("the schema is not implementable: %v", diags)
		}
	}

	for _, build := range []func() fwdatasource.DataSource{
		NewWebhookDataSource,
		NewWebhooksDataSource,
		NewStatusPageDataSource,
		NewStatusPagesDataSource,
	} {
		resp := &fwdatasource.SchemaResponse{}
		build().Schema(ctx, fwdatasource.SchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("building the schema: %v", resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("the schema is not implementable: %v", diags)
		}
	}
}

func scopeObject(t *testing.T, values map[string]attr.Value) types.Object {
	t.Helper()
	full := map[string]attr.Value{
		"all":         types.BoolNull(),
		"monitor_ids": types.SetNull(types.StringType),
		"tags":        types.SetNull(types.StringType),
	}
	for name, value := range values {
		full[name] = value
	}
	object, diags := types.ObjectValue(webhookScopeAttrTypes, full)
	if diags.HasError() {
		t.Fatalf("building the scope: %v", diags)
	}
	return object
}

func headerSet(t *testing.T, pairs ...[2]string) types.Set {
	t.Helper()
	values := make([]attr.Value, 0, len(pairs))
	for _, pair := range pairs {
		object, diags := types.ObjectValue(webhookHeaderAttrTypes, map[string]attr.Value{
			"header": types.StringValue(pair[0]),
			"value":  types.StringValue(pair[1]),
		})
		if diags.HasError() {
			t.Fatalf("building the header: %v", diags)
		}
		values = append(values, object)
	}
	set, diags := types.SetValue(webhookHeaderObjectType, values)
	if diags.HasError() {
		t.Fatalf("building the header set: %v", diags)
	}
	return set
}

func baseWebhookModel(t *testing.T) webhookModel {
	t.Helper()
	return webhookModel{
		ID:           types.StringValue(testWebhookID),
		URL:          types.StringValue(testWebhookAddr),
		Name:         types.StringValue("Ops channel"),
		Events:       stringSetValue(t, "monitor.up", "monitor.down"),
		Scope:        scopeObject(t, map[string]attr.Value{"all": types.BoolValue(true)}),
		Headers:      types.SetNull(webhookHeaderObjectType),
		Enabled:      types.BoolValue(true),
		Secret:       types.StringNull(),
		RotateSecret: types.Int64Null(),
	}
}

func TestEncodeWebhookBuildsTheWireBody(t *testing.T) {
	ctx := context.Background()
	model := baseWebhookModel(t)
	model.Headers = headerSet(t, [2]string{"X-Token", "s3cret"}, [2]string{"Accept", "application/json"})

	body, diags := encodeWebhook(ctx, &model)
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}

	want := map[string]any{
		"url":     testWebhookAddr,
		"name":    "Ops channel",
		"enabled": true,
		// Sorted, so that two spellings of one set compare equal.
		"events": []any{"monitor.down", "monitor.up"},
		"scope":  map[string]any{"all": true},
		"headers": []any{
			map[string]any{"header": "Accept", "value": "application/json"},
			map[string]any{"header": "X-Token", "value": "s3cret"},
		},
	}
	if got := normalizeWire(body); !reflect.DeepEqual(got, normalizeWire(want)) {
		t.Fatalf("wire body\n got: %v\nwant: %v", got, want)
	}
}

func TestEncodeWebhookRendersEachScopeForm(t *testing.T) {
	ctx := context.Background()

	byMonitors := baseWebhookModel(t)
	byMonitors.Scope = scopeObject(t, map[string]attr.Value{
		"monitor_ids": stringSetValue(t, "b-id", "a-id"),
	})
	body, _ := encodeWebhook(ctx, &byMonitors)
	scope, _ := normalizeWire(body)["scope"].(map[string]any)
	if !reflect.DeepEqual(scope, map[string]any{"monitorIds": []any{"a-id", "b-id"}}) {
		t.Fatalf("expected a sorted monitor list, got %v", scope)
	}

	byTags := baseWebhookModel(t)
	byTags.Scope = scopeObject(t, map[string]attr.Value{"tags": stringSetValue(t, "prod")})
	body, _ = encodeWebhook(ctx, &byTags)
	scope, _ = normalizeWire(body)["scope"].(map[string]any)
	if !reflect.DeepEqual(scope, map[string]any{"tags": []any{"prod"}}) {
		t.Fatalf("expected a tag scope, got %v", scope)
	}
}

func TestWebhookPatchSendsOnlyWhatChanged(t *testing.T) {
	ctx := context.Background()

	state := baseWebhookModel(t)
	plan := baseWebhookModel(t)
	plan.Name = types.StringValue("Paging channel")

	have, _ := encodeWebhook(ctx, &state)
	want, _ := encodeWebhook(ctx, &plan)
	body := client.Diff(have, want)

	if len(body) != 1 || body["name"] != "Paging channel" {
		t.Fatalf("expected only the name to travel, got %v", body)
	}
}

func TestWebhookPatchSendsAChangedScopeWhole(t *testing.T) {
	ctx := context.Background()

	state := baseWebhookModel(t)
	plan := baseWebhookModel(t)
	plan.Scope = scopeObject(t, map[string]attr.Value{"tags": stringSetValue(t, "prod")})

	have, _ := encodeWebhook(ctx, &state)
	want, _ := encodeWebhook(ctx, &plan)
	body := client.Diff(have, want)

	scope, ok := body["scope"].(map[string]any)
	if !ok {
		t.Fatalf("expected the scope to travel, got %v", body)
	}
	if _, hasAll := scope["all"]; hasAll {
		t.Fatalf("a changed scope is sent whole, in one form only: %v", scope)
	}
}

func TestWebhookToStateTakesTheMintedSecret(t *testing.T) {
	ctx := context.Background()
	prior := baseWebhookModel(t)

	state, diags := webhookToState(ctx, &prior, &client.Webhook{
		ID:      testWebhookID,
		URL:     testWebhookAddr,
		Events:  []string{"monitor.down"},
		Enabled: true,
		Scope:   &client.WebhookScope{All: boolOf(true), MonitorCount: 8},
		Secret: &client.WebhookSecret{
			Set:       true,
			UpdatedAt: int64Of(1785712680),
			Value:     "whsec_minted",
		},
	})
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if state.Secret.ValueString() != "whsec_minted" {
		t.Fatalf("expected the minted secret in state, got %v", state.Secret)
	}
	if !state.SecretSet.ValueBool() || state.SecretUpdatedAt.ValueInt64() != 1785712680 {
		t.Fatalf("expected the secret metadata, got %v %v", state.SecretSet, state.SecretUpdatedAt)
	}
	if state.MonitorCount.ValueInt64() != 8 {
		t.Fatalf("expected the resolved count, got %v", state.MonitorCount)
	}
}

func TestWebhookToStateKeepsTheSecretOnARead(t *testing.T) {
	ctx := context.Background()
	prior := baseWebhookModel(t)
	prior.Secret = types.StringValue("whsec_minted-a-while-ago")
	prior.RotateSecret = types.Int64Value(2)

	// A read publishes only whether a secret is set, never its value.
	state, diags := webhookToState(ctx, &prior, &client.Webhook{
		ID:      testWebhookID,
		URL:     testWebhookAddr,
		Events:  []string{"monitor.down"},
		Enabled: true,
		Scope:   &client.WebhookScope{All: boolOf(true)},
		Secret:  &client.WebhookSecret{Set: true, UpdatedAt: int64Of(1785712680)},
	})
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if state.Secret.ValueString() != "whsec_minted-a-while-ago" {
		t.Fatalf("expected the held secret to survive a read, got %v", state.Secret)
	}
	if state.RotateSecret.ValueInt64() != 2 {
		t.Fatalf("expected the rotation counter to survive, got %v", state.RotateSecret)
	}
}

func TestWebhookToStateLeavesAnImportedSecretNull(t *testing.T) {
	ctx := context.Background()

	state, diags := webhookToState(ctx, nil, &client.Webhook{
		ID:      testWebhookID,
		URL:     testWebhookAddr,
		Events:  []string{"monitor.down"},
		Enabled: true,
		Secret:  &client.WebhookSecret{Set: true},
	})
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if !state.Secret.IsNull() {
		t.Fatalf("expected an adopted webhook to hold no secret, got %v", state.Secret)
	}
	if !state.SecretSet.ValueBool() {
		t.Fatalf("expected the webhook to report that it has one, got %v", state.SecretSet)
	}
}

func TestWebhookToStateRestoresAWithheldHeaderValue(t *testing.T) {
	ctx := context.Background()
	prior := baseWebhookModel(t)
	prior.Headers = headerSet(t, [2]string{"X-Token", "s3cret"})

	state, diags := webhookToState(ctx, &prior, &client.Webhook{
		ID:      testWebhookID,
		URL:     testWebhookAddr,
		Events:  []string{"monitor.down"},
		Enabled: true,
		Headers: []client.WebhookHeader{{Header: "X-Token"}},
	})
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}

	var headers []webhookHeaderModel
	state.Headers.ElementsAs(ctx, &headers, false)
	if len(headers) != 1 || headers[0].Value.ValueString() != "s3cret" {
		t.Fatalf("expected the written value to be kept, got %+v", headers)
	}
}

func TestRotationRequested(t *testing.T) {
	cases := []struct {
		name    string
		planned types.Int64
		current types.Int64
		want    bool
	}{
		{"unset stays unset", types.Int64Null(), types.Int64Null(), false},
		{"first counter", types.Int64Value(1), types.Int64Null(), true},
		{"same counter", types.Int64Value(1), types.Int64Value(1), false},
		{"bumped counter", types.Int64Value(2), types.Int64Value(1), true},
		{"lowered counter still rotates", types.Int64Value(1), types.Int64Value(2), true},
		{"removed counter does not rotate", types.Int64Null(), types.Int64Value(2), false},
		{"unknown counter rotates", types.Int64Unknown(), types.Int64Value(1), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rotationRequested(c.planned, c.current); got != c.want {
				t.Fatalf("expected %v, got %v", c.want, got)
			}
		})
	}
}

func TestValidateWebhookScope(t *testing.T) {
	cases := []struct {
		name      string
		scope     types.Object
		wantError bool
	}{
		{"all", scopeObject(t, map[string]attr.Value{"all": types.BoolValue(true)}), false},
		{"monitors", scopeObject(t, map[string]attr.Value{"monitor_ids": stringSetValue(t, "a")}), false},
		{"tags", scopeObject(t, map[string]attr.Value{"tags": stringSetValue(t, "prod")}), false},
		{"all false", scopeObject(t, map[string]attr.Value{"all": types.BoolValue(false)}), true},
		{"nothing", scopeObject(t, nil), true},
		{"two forms", scopeObject(t, map[string]attr.Value{
			"all":  types.BoolValue(true),
			"tags": stringSetValue(t, "prod"),
		}), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diags := validateWebhookScope(c.scope)
			if diags.HasError() != c.wantError {
				t.Fatalf("expected error=%v, got %v", c.wantError, diags)
			}
		})
	}
}

func TestWebhookPointerMapperPlacesFailuresOnAttributes(t *testing.T) {
	cases := map[string]string{
		"/url":              "url",
		"/events":           "events",
		"/scope/monitorIds": "scope.monitor_ids",
		"/scope/tags":       "scope.tags",
		"/headers":          "headers",
	}
	for pointer, want := range cases {
		got, ok := webhookPointerMapper(pointer)
		if !ok {
			t.Fatalf("expected %s to map somewhere", pointer)
		}
		if got.String() != want {
			t.Fatalf("expected %s to map to %s, got %s", pointer, want, got)
		}
	}
	if _, ok := webhookPointerMapper("/somethingElse"); ok {
		t.Fatal("an unrecognised pointer belongs on the resource, not an attribute")
	}
}

func TestHTTPSURLPattern(t *testing.T) {
	good := []string{
		"https://hooks.example.com/host-tracker",
		"https://example.com",
		"https://example.com:8443/hook?token=1",
	}
	for _, url := range good {
		if !httpsURLPattern.MatchString(url) {
			t.Fatalf("expected %q to be accepted", url)
		}
	}
	bad := []string{"http://hooks.example.com/x", "hooks.example.com", "ftp://example.com", ""}
	for _, url := range bad {
		if httpsURLPattern.MatchString(url) {
			t.Fatalf("expected %q to be refused", url)
		}
	}
}

func boolOf(v bool) *bool    { return &v }
func int64Of(v int64) *int64 { return &v }

func TestWebhookToStateKeepsAnExplicitlyEmptyHeaderSet(t *testing.T) {
	ctx := context.Background()

	// `headers = []` clears them, and the answer carries none. Reading that
	// back as null would make the apply inconsistent with the plan.
	prior := baseWebhookModel(t)
	prior.Headers = headerSet(t)

	state, diags := webhookToState(ctx, &prior, &client.Webhook{
		ID:      testWebhookID,
		URL:     testWebhookAddr,
		Events:  []string{"monitor.down"},
		Enabled: true,
	})
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if state.Headers.IsNull() {
		t.Fatal("an explicitly empty set stays an empty set")
	}
	if len(state.Headers.Elements()) != 0 {
		t.Fatalf("expected no headers, got %v", state.Headers)
	}

	// A configuration that never mentioned headers keeps reading null.
	silent := baseWebhookModel(t)
	state, diags = webhookToState(ctx, &silent, &client.Webhook{
		ID: testWebhookID, URL: testWebhookAddr, Events: []string{"monitor.down"}, Enabled: true,
	})
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if !state.Headers.IsNull() {
		t.Fatalf("expected null, got %v", state.Headers)
	}
}
