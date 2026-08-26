package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// partialObject fills the members a test names and leaves the rest null,
// which is what a configuration that writes a few settings looks like.
func partialObject(t *testing.T, attrTypes map[string]attr.Type, values map[string]attr.Value) types.Object {
	t.Helper()
	complete := make(map[string]attr.Value, len(attrTypes))
	for name, attrType := range attrTypes {
		if value, ok := values[name]; ok {
			complete[name] = value
			continue
		}
		complete[name] = nullOf(attrType)
	}
	object, diags := types.ObjectValue(attrTypes, complete)
	if diags.HasError() {
		t.Fatalf("building the object: %v", diags)
	}
	return object
}

func settingsObject(t *testing.T, branchName string, values map[string]attr.Value) types.Object {
	t.Helper()
	branch, ok := branchByName(branchName)
	if !ok {
		t.Fatalf("no settings branch named %q", branchName)
	}
	inner := partialObject(t, branchAttrTypes(branch), values)

	members := map[string]attr.Value{}
	for _, b := range settingsBranches {
		if b.name == branchName {
			members[b.name] = inner
			continue
		}
		members[b.name] = types.ObjectNull(branchAttrTypes(b))
	}
	object, diags := types.ObjectValue(settingsAttrTypes(), members)
	if diags.HasError() {
		t.Fatalf("building the settings object: %v", diags)
	}
	return object
}

func stringSetValue(t *testing.T, values ...string) types.Set {
	t.Helper()
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.StringValue(v))
	}
	set, diags := types.SetValue(types.StringType, elements)
	if diags.HasError() {
		t.Fatalf("building the set: %v", diags)
	}
	return set
}

func int64SetValue(t *testing.T, values ...int64) types.Set {
	t.Helper()
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.Int64Value(v))
	}
	set, diags := types.SetValue(types.Int64Type, elements)
	if diags.HasError() {
		t.Fatalf("building the set: %v", diags)
	}
	return set
}

// roundTrip is the property every settings branch must hold: what a
// configuration writes reaches the wire under the API's own member names,
// and what the API answers rebuilds the same object.
func roundTrip(t *testing.T, branchName, monitorType string, values map[string]attr.Value, wantWire map[string]any) {
	t.Helper()
	ctx := context.Background()

	settings := settingsObject(t, branchName, values)
	wire, diags := encodeSettings(ctx, settings, monitorType)
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	if !reflect.DeepEqual(normalizeWire(wire), normalizeWire(wantWire)) {
		t.Fatalf("wire mismatch\n got: %#v\nwant: %#v", wire, wantWire)
	}

	decoded, diags := decodeSettings(ctx, settings, jsonRound(t, wire), monitorType)
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if !decoded.Equal(settings) {
		t.Fatalf("round trip changed the object\n got: %s\nwant: %s", decoded, settings)
	}
}

func TestHttpSettingsRoundTrip(t *testing.T) {
	headerType := types.ObjectType{AttrTypes: headerAttrTypes}
	header, diags := types.ObjectValue(headerAttrTypes, map[string]attr.Value{
		"name":  types.StringValue("X-Api-Key"),
		"value": types.StringValue("secret"),
	})
	if diags.HasError() {
		t.Fatalf("building the header: %v", diags)
	}
	headers, diags := types.ListValue(headerType, []attr.Value{header})
	if diags.HasError() {
		t.Fatalf("building the header list: %v", diags)
	}

	roundTrip(t, "http", "http", map[string]attr.Value{
		"method":              types.StringValue("P"),
		"keywords":            types.StringValue("Sign in"),
		"keyword_mode":        types.StringValue("PresentAll"),
		"timeout":             types.Int64Value(20000),
		"username":            types.StringValue("ops"),
		"password":            types.StringValue("hunter2"),
		"auth_schema":         types.StringValue("Basic"),
		"headers":             headers,
		"body":                types.StringValue(`{"ok":true}`),
		"post_parameters":     types.StringValue("a=1"),
		"ignored_statuses":    int64SetValue(t, 404),
		"error_statuses":      int64SetValue(t, 500, 503),
		"follow_redirect":     types.BoolValue(false),
		"max_redirects":       types.Int64Value(3),
		"error_on_redirect":   types.BoolValue(true),
		"user_agent":          types.StringValue("terraform"),
		"accept":              types.StringValue("application/json"),
		"referer":             types.StringValue("https://example.com"),
		"max_size":            types.Int64Value(65536),
		"dns":                 stringSetValue(t, "8.8.8.8"),
		"public_dns":          types.Int64Value(1),
		"dns_no_cache":        types.BoolValue(true),
		"expected_dns":        stringSetValue(t, "9.9.9.9"),
		"expected_ips":        stringSetValue(t, "203.0.113.10"),
		"require_valid_chain": types.BoolValue(true),
		"check_revocation":    types.BoolValue(true),
		"require_strong_tls":  types.BoolValue(true),
		"block_weak_ciphers":  types.BoolValue(true),
		"cert_watch_days":     int64SetValue(t, 7, 30),
		"assert_mode":         types.BoolValue(false),
		"preset":              types.StringValue("bl:ru"),
	}, map[string]any{
		"method":            "P",
		"keywords":          "Sign in",
		"keywordMode":       "PresentAll",
		"timeout":           int64(20000),
		"username":          "ops",
		"password":          "hunter2",
		"authSchema":        "Basic",
		"headers":           []any{map[string]any{"name": "X-Api-Key", "value": "secret"}},
		"body":              `{"ok":true}`,
		"postParameters":    "a=1",
		"ignoredStatuses":   []int64{404},
		"errorStatuses":     []int64{500, 503},
		"followRedirect":    false,
		"maxRedirects":      int64(3),
		"errorOnRedirect":   true,
		"userAgent":         "terraform",
		"accept":            "application/json",
		"referer":           "https://example.com",
		"maxSize":           int64(65536),
		"dns":               []string{"8.8.8.8"},
		"publicDns":         int64(1),
		"dnsNoCache":        true,
		"expectedDns":       []string{"9.9.9.9"},
		"expectedIps":       []string{"203.0.113.10"},
		"requireValidChain": true,
		"checkRevocation":   true,
		"requireStrongTls":  true,
		"blockWeakCiphers":  true,
		"certWatchDays":     []int64{7, 30},
		"assertMode":        false,
		"preset":            "bl:ru",
	})
}

func TestPingSettingsRoundTrip(t *testing.T) {
	roundTrip(t, "ping", "ping", map[string]attr.Value{
		"dns":          stringSetValue(t, "1.1.1.1"),
		"public_dns":   types.Int64Value(0),
		"expected_ips": stringSetValue(t, "203.0.113.1"),
	}, map[string]any{
		"dns":         []string{"1.1.1.1"},
		"publicDns":   int64(0),
		"expectedIps": []string{"203.0.113.1"},
	})
}

func TestPortSettingsRoundTrip(t *testing.T) {
	roundTrip(t, "port", "port", map[string]attr.Value{
		"pattern":            types.StringValue("SSH-2.0"),
		"ssl":                types.BoolValue(true),
		"require_strong_tls": types.BoolValue(true),
		"cert_watch_days":    int64SetValue(t, 14),
	}, map[string]any{
		"pattern":          "SSH-2.0",
		"ssl":              true,
		"requireStrongTls": true,
		"certWatchDays":    []int64{14},
	})
}

func TestDnsblSettingsRoundTrip(t *testing.T) {
	roundTrip(t, "dnsbl", "dnsbl", map[string]attr.Value{
		"scope": types.StringValue("webAndMx"),
	}, map[string]any{"scope": "webAndMx"})
}

func TestEmptyBranchesRoundTrip(t *testing.T) {
	// The API defines no members for these three today. The blocks still
	// have to survive a round trip, empty.
	for _, pair := range [][2]string{{"ssl_exp", "sslExp"}, {"domain_exp", "domainExp"}, {"web_risk", "webRisk"}} {
		roundTrip(t, pair[0], pair[1], map[string]attr.Value{}, map[string]any{})
	}
}

func TestAttachedSubChecksRoundTrip(t *testing.T) {
	ctx := context.Background()
	branch, _ := branchByName("http")

	dnsbl := partialObject(t, fieldsAttrTypes(attachedKinds["dnsbl"].fields), map[string]attr.Value{
		"enabled": types.BoolValue(true),
	})
	webRisk := partialObject(t, fieldsAttrTypes(attachedKinds["web_risk"].fields), map[string]attr.Value{
		"enabled":  types.BoolValue(true),
		"interval": types.Int64Value(3600),
	})
	attached := partialObject(t, attachedAttrTypes(branch.attached), map[string]attr.Value{
		"dnsbl":    dnsbl,
		"web_risk": webRisk,
	})

	settings := settingsObject(t, "http", map[string]attr.Value{"attached": attached})
	wire, diags := encodeSettings(ctx, settings, "http")
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}

	got, ok := wire["attached"].(map[string]any)
	if !ok {
		t.Fatalf("expected an attached object, got %#v", wire["attached"])
	}
	if _, present := got["sslExp"]; present {
		t.Fatalf("expected an unset sub-check to stay off the wire, got %#v", got)
	}
	if webRiskWire, ok := got["webRisk"].(map[string]any); !ok || webRiskWire["interval"] != int64(3600) {
		t.Fatalf("expected the web risk interval on the wire, got %#v", got["webRisk"])
	}

	decoded, diags := decodeSettings(ctx, settings, jsonRound(t, wire), "http")
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if !decoded.Equal(settings) {
		t.Fatalf("round trip changed the attached blocks\n got: %s\nwant: %s", decoded, settings)
	}
}

func TestDecodeKeepsADefaultTheAPIDoesNotWriteBack(t *testing.T) {
	ctx := context.Background()
	// The API leaves timeout out of a read while it holds the default, so
	// the value that was applied has to survive the read.
	prior := settingsObject(t, "http", map[string]attr.Value{
		"timeout":  types.Int64Value(40000),
		"keywords": types.StringValue("Sign in"),
	})

	decoded, diags := decodeSettings(ctx, prior, map[string]any{"keywords": "Sign in"}, "http")
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	branch := decoded.Attributes()["http"].(types.Object)
	if timeout := branch.Attributes()["timeout"].(types.Int64); timeout.IsNull() || timeout.ValueInt64() != 40000 {
		t.Fatalf("expected the applied default to survive the read, got %v", timeout)
	}
}

func TestDecodeReportsAClearedMemberThatIsNotDefaulted(t *testing.T) {
	ctx := context.Background()
	prior := settingsObject(t, "http", map[string]attr.Value{"keywords": types.StringValue("Sign in")})

	decoded, diags := decodeSettings(ctx, prior, map[string]any{}, "http")
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	branch := decoded.Attributes()["http"].(types.Object)
	if keywords := branch.Attributes()["keywords"].(types.String); !keywords.IsNull() {
		t.Fatalf("expected a removed keyword list to read back as drift, got %v", keywords)
	}
}

func TestDecodeKeepsAWriteOnlyMember(t *testing.T) {
	ctx := context.Background()
	// The API compiles asserts_source into rows and never publishes it.
	prior := settingsObject(t, "http", map[string]attr.Value{
		"asserts_source": types.StringValue("status eq 200"),
	})

	decoded, diags := decodeSettings(ctx, prior, map[string]any{"assertsText": "status eq 200"}, "http")
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	branch := decoded.Attributes()["http"].(types.Object)
	if source := branch.Attributes()["asserts_source"].(types.String); source.ValueString() != "status eq 200" {
		t.Fatalf("expected the configured source to stay in state, got %v", source)
	}
	if text := branch.Attributes()["asserts_text"].(types.String); text.ValueString() != "status eq 200" {
		t.Fatalf("expected the stored twin to be read back, got %v", text)
	}
}

func TestEncodeSkipsTheServerOwnedMember(t *testing.T) {
	ctx := context.Background()
	settings := settingsObject(t, "http", map[string]attr.Value{
		"asserts_text": types.StringValue("status eq 200"),
	})

	wire, diags := encodeSettings(ctx, settings, "http")
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	if _, present := wire["assertsText"]; present {
		t.Fatalf("expected a server-owned member to stay off a write, got %#v", wire)
	}
}

func TestEncodeIgnoresABranchThatIsNotTheMonitorsType(t *testing.T) {
	ctx := context.Background()
	settings := settingsObject(t, "ping", map[string]attr.Value{"public_dns": types.Int64Value(1)})

	wire, diags := encodeSettings(ctx, settings, "http")
	if diags.HasError() {
		t.Fatalf("encoding: %v", diags)
	}
	if len(wire) != 0 {
		t.Fatalf("expected nothing to be encoded for the wrong type, got %#v", wire)
	}
}

func TestDecodeLeavesAnUnknownTypeToSettingsJSON(t *testing.T) {
	ctx := context.Background()
	decoded, diags := decodeSettings(ctx, types.ObjectNull(settingsAttrTypes()), map[string]any{"query": "select 1"}, "database")
	if diags.HasError() {
		t.Fatalf("decoding: %v", diags)
	}
	if !decoded.IsNull() {
		t.Fatalf("expected a type with no branch to leave settings null, got %s", decoded)
	}
}

// jsonRound puts a wire map through the encoding a real answer goes
// through, so that the decoders are tested against float64 numbers and
// []any lists rather than the Go values the encoder produced.
func jsonRound(t *testing.T, wire map[string]any) map[string]any {
	t.Helper()
	if len(wire) == 0 {
		return map[string]any{}
	}
	return mustJSONRound(t, wire)
}

func normalizeWire(wire map[string]any) map[string]any {
	return mustNormalize(wire)
}
