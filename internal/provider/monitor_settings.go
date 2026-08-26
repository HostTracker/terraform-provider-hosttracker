package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// fieldKind is how one settings member is spelled in a configuration.
type fieldKind int

const (
	kindString fieldKind = iota
	kindBool
	kindInt
	kindStringSet
	kindIntSet
	kindHeaders
)

// settingField is one member of one monitor type's settings object.
type settingField struct {
	// name is the attribute a configuration writes.
	name string
	// wire is the member name the API takes.
	wire string
	kind fieldKind
	// sensitive keeps the value out of plan output and console rendering.
	sensitive bool
	// defaulted marks a member the API leaves out of a read while it holds
	// its default. Such a member keeps its planned value in state rather
	// than reading back as null, which would otherwise make every apply
	// report a change it did not make.
	defaulted bool
	// writeOnly marks a member the API accepts and never publishes back.
	writeOnly bool
	// serverOwned marks a member the API writes and a configuration cannot.
	serverOwned bool
	desc        string
}

// settingsBranch is one monitor type's settings object.
type settingsBranch struct {
	// name is the attribute under `settings`.
	name string
	// wireType is the monitor type token this branch describes.
	wireType string
	desc     string
	fields   []settingField
	// attached names the sub-checks that may ride on this type.
	attached []string
}

const (
	dnsHelp         = "Resolver addresses to use instead of the monitoring location's own. At most 4."
	expectedDNSHelp = "Resolver addresses the lookup is expected to come from. At most 10."
	expectedIPsHelp = "Addresses the host is expected to resolve to. Anything else fails the check. At most 10."
	publicDNSHelp   = "Run from public-DNS-filtered locations. `0` means the setting is not in force."
	certWatchHelp   = "Days-before-expiry thresholds to alert on for the certificate this endpoint serves. At most 8 entries, each between 1 and 3650."
)

// tlsPolicyFields are the certificate rules the http and port types share.
func tlsPolicyFields() []settingField {
	return []settingField{
		{name: "require_valid_chain", wire: "requireValidChain", kind: kindBool, defaulted: true,
			desc: "Fail the check unless the server presents a complete, trusted certificate chain. Off by default, which is why a self-signed certificate does not fail a check on its own."},
		{name: "check_revocation", wire: "checkRevocation", kind: kindBool, defaulted: true,
			desc: "Fail the check when the certificate has been revoked, verified online during the handshake. Sold separately from the rest of the SSL policy."},
		{name: "require_strong_tls", wire: "requireStrongTls", kind: kindBool, defaulted: true,
			desc: "Fail the check unless the connection negotiates TLS 1.2 or newer."},
		{name: "block_weak_ciphers", wire: "blockWeakCiphers", kind: kindBool, defaulted: true,
			desc: "Fail the check when the negotiated cipher suite is 128-bit or weaker."},
		{name: "cert_watch_days", wire: "certWatchDays", kind: kindIntSet, desc: certWatchHelp},
	}
}

// resolutionFields are the DNS-resolution members shared by the types that
// resolve a host themselves.
func resolutionFields(withCache bool) []settingField {
	out := []settingField{
		{name: "dns", wire: "dns", kind: kindStringSet, desc: dnsHelp},
		{name: "public_dns", wire: "publicDns", kind: kindInt, defaulted: true, desc: publicDNSHelp},
		{name: "expected_dns", wire: "expectedDns", kind: kindStringSet, desc: expectedDNSHelp},
		{name: "expected_ips", wire: "expectedIps", kind: kindStringSet, desc: expectedIPsHelp},
	}
	if withCache {
		out = append(out, settingField{name: "dns_no_cache", wire: "dnsNoCache", kind: kindBool, defaulted: true,
			desc: "Bypass the monitoring location's DNS cache."})
	}
	return out
}

// settingsBranches lists the seven monitor types whose settings a
// configuration can spell out as a typed block. Every other type -
// waterfall, tran, api, database, counter, snmp, cntCheck - is written
// through `settings_json` until it grows a branch of its own.
var settingsBranches = []settingsBranch{
	{
		name:     "http",
		wireType: "http",
		desc:     "Settings for an `http` monitor: fetch a url and judge the answer.",
		attached: []string{"dnsbl", "ssl_exp", "domain_exp", "web_risk"},
		fields: append(append([]settingField{
			{name: "method", wire: "method", kind: kindString, defaulted: true,
				desc: "The HTTP method, as the single letter the executor takes: `G` (GET, the default), `H`, `P` (POST), `U` (PUT), `D` (DELETE), `A` (PATCH)."},
			{name: "keywords", wire: "keywords", kind: kindString,
				desc: "Comma-separated keywords matched against the response body. The whole list is capped at 255 characters."},
			{name: "keyword_mode", wire: "keywordMode", kind: kindString, defaulted: true,
				desc: "How `keywords` decides the verdict: `PresentAny` (the default), `PresentAll`, `ReverseAny`, `ReverseAll`, `ReverseWithResult`. Read only when `keywords` is set."},
			{name: "timeout", wire: "timeout", kind: kindInt, defaulted: true,
				desc: "Request timeout in milliseconds. 40000 by default, capped at 100000."},
			{name: "username", wire: "username", kind: kindString, desc: "HTTP basic auth user name."},
			{name: "password", wire: "password", kind: kindString, sensitive: true,
				desc: "HTTP basic auth password. Send it again to change it."},
			{name: "auth_schema", wire: "authSchema", kind: kindString, desc: "The authentication scheme. Only `Basic` is accepted."},
			{name: "headers", wire: "headers", kind: kindHeaders,
				desc: "Extra request headers, in order. Combined name and value length across all headers is capped at 1023 characters; `connection`, `content-length` and `date` are dropped."},
			{name: "body", wire: "body", kind: kindString, desc: "The raw request body."},
			{name: "post_parameters", wire: "postParameters", kind: kindString, desc: "Form-encoded POST parameters."},
			{name: "ignored_statuses", wire: "ignoredStatuses", kind: kindIntSet, desc: "HTTP status codes that must not fail the check. At most 20."},
			{name: "error_statuses", wire: "errorStatuses", kind: kindIntSet, desc: "HTTP status codes that must fail the check. At most 20."},
			{name: "follow_redirect", wire: "followRedirect", kind: kindBool, defaulted: true, desc: "Follow 3xx redirects. On by default."},
			{name: "max_redirects", wire: "maxRedirects", kind: kindInt, defaulted: true, desc: "How many redirect hops to follow. 20 is both the ceiling and the default."},
			{name: "error_on_redirect", wire: "errorOnRedirect", kind: kindBool, defaulted: true, desc: "Treat a 3xx as a failure."},
			{name: "user_agent", wire: "userAgent", kind: kindString, desc: "The User-Agent header to send."},
			{name: "accept", wire: "accept", kind: kindString, desc: "The Accept header to send."},
			{name: "referer", wire: "referer", kind: kindString, desc: "The Referer header to send."},
			{name: "max_size", wire: "maxSize", kind: kindInt, defaulted: true, desc: "The largest response body to download, in bytes."},
		}, resolutionFields(true)...), append(tlsPolicyFields(), []settingField{
			{name: "assert_mode", wire: "assertMode", kind: kindBool, defaulted: true,
				desc: "Judge the response by the assertion list rather than by the keyword and status members, which are refused while it is on. Transport failures still fail the check either way."},
			{name: "asserts_source", wire: "assertsSource", kind: kindString, writeOnly: true,
				desc: "The assertion list as AssertRuleLang source text, one rule per line (`status eq 200`). It is parsed and stored as rows, so the API never reads it back: what is in the configuration stays in state. Read `asserts_text` for what the API actually stored."},
			{name: "asserts_text", wire: "assertsText", kind: kindString, serverOwned: true,
				desc: "The stored assertion rows rendered back as source text. Written by the API from what `asserts_source` compiled to."},
			{name: "preset", wire: "preset", kind: kindString,
				desc: "A server-built settings preset. `bl:ru` builds the whole settings object for a Russian-registry blacklist check and pins the monitoring locations."},
		}...)...),
	},
	{
		name:     "ping",
		wireType: "ping",
		desc:     "Settings for a `ping` monitor: ICMP echo from the chosen locations.",
		attached: []string{"dnsbl"},
		fields:   resolutionFields(false),
	},
	{
		name:     "port",
		wireType: "port",
		desc:     "Settings for a `port` monitor: open a TCP connection, optionally over TLS, and look at the banner.",
		attached: []string{"dnsbl"},
		fields: append(append([]settingField{
			{name: "pattern", wire: "pattern", kind: kindString, desc: "Text expected in the banner the port returns."},
			{name: "ssl", wire: "ssl", kind: kindBool, defaulted: true, desc: "Negotiate TLS on the connection."},
		}, resolutionFields(false)...), tlsPolicyFields()...),
	},
	{
		name:     "dnsbl",
		wireType: "dnsbl",
		desc:     "Settings for a standalone `dnsbl` monitor: look the host up in the DNS blacklists. It runs from a fixed internal network, so it takes no `locations`.",
		fields: []settingField{
			{name: "scope", wire: "scope", kind: kindString, defaulted: true,
				desc: "Which addresses of the host are looked up: `firstWebIp` (the default), `allWebIps`, or `webAndMx`."},
		},
	},
	{
		name:     "ssl_exp",
		wireType: "sslExp",
		desc:     "Settings for a standalone `sslExp` monitor, which watches a certificate's expiry. The API defines no members for it today; the block exists so that one it grows later is a plain attribute. It runs from a fixed internal network, so it takes no `locations`.",
	},
	{
		name:     "domain_exp",
		wireType: "domainExp",
		desc:     "Settings for a standalone `domainExp` monitor, which watches a registration's expiry. The API defines no members for it today. It runs from a fixed internal network, so it takes no `locations`.",
	},
	{
		name:     "web_risk",
		wireType: "webRisk",
		desc:     "Settings for a standalone `webRisk` monitor, which checks a url against Google's Web Risk lists. The API defines no members for it today. It runs from a fixed internal network, so it takes no `locations`.",
	},
}

// attachedKinds describes the four sub-checks that can ride on a parent
// monitor. Their wire names sit under `settings.attached`.
var attachedKinds = map[string]struct {
	wire   string
	desc   string
	fields []settingField
}{
	"dnsbl": {wire: "dnsbl", desc: "The blacklist sub-check riding on this monitor.", fields: []settingField{
		{name: "enabled", wire: "enabled", kind: kindBool, defaulted: true, desc: "Whether the sub-check runs."},
	}},
	"ssl_exp": {wire: "sslExp", desc: "The certificate-expiry sub-check riding on this monitor. Its thresholds are `cert_watch_days`.", fields: []settingField{
		{name: "enabled", wire: "enabled", kind: kindBool, defaulted: true, desc: "Whether the sub-check runs."},
	}},
	"domain_exp": {wire: "domainExp", desc: "The domain-expiry sub-check riding on this monitor.", fields: []settingField{
		{name: "enabled", wire: "enabled", kind: kindBool, defaulted: true, desc: "Whether the sub-check runs."},
	}},
	"web_risk": {wire: "webRisk", desc: "The Web Risk sub-check riding on this monitor.", fields: []settingField{
		{name: "enabled", wire: "enabled", kind: kindBool, defaulted: true, desc: "Whether the sub-check runs."},
		{name: "interval", wire: "interval", kind: kindInt, defaulted: true, desc: "Seconds between Web Risk lookups. 43200 by default."},
	}},
}

func branchByName(name string) (settingsBranch, bool) {
	for _, b := range settingsBranches {
		if b.name == name {
			return b, true
		}
	}
	return settingsBranch{}, false
}

// branchForType finds the settings branch that describes a monitor type,
// which is how the union is resolved: the type picks the branch.
func branchForType(monitorType string) (settingsBranch, bool) {
	for _, b := range settingsBranches {
		if b.wireType == monitorType {
			return b, true
		}
	}
	return settingsBranch{}, false
}

// headerAttrTypes is the object one request header is spelled as.
var headerAttrTypes = map[string]attr.Type{
	"name":  types.StringType,
	"value": types.StringType,
}

func fieldType(f settingField) attr.Type {
	switch f.kind {
	case kindBool:
		return types.BoolType
	case kindInt:
		return types.Int64Type
	case kindStringSet:
		return types.SetType{ElemType: types.StringType}
	case kindIntSet:
		return types.SetType{ElemType: types.Int64Type}
	case kindHeaders:
		return types.ListType{ElemType: types.ObjectType{AttrTypes: headerAttrTypes}}
	default:
		return types.StringType
	}
}

func fieldsAttrTypes(fields []settingField) map[string]attr.Type {
	out := make(map[string]attr.Type, len(fields))
	for _, f := range fields {
		out[f.name] = fieldType(f)
	}
	return out
}

func attachedAttrTypes(kinds []string) map[string]attr.Type {
	out := make(map[string]attr.Type, len(kinds))
	for _, k := range kinds {
		out[k] = types.ObjectType{AttrTypes: fieldsAttrTypes(attachedKinds[k].fields)}
	}
	return out
}

func branchAttrTypes(b settingsBranch) map[string]attr.Type {
	out := fieldsAttrTypes(b.fields)
	if len(b.attached) > 0 {
		out["attached"] = types.ObjectType{AttrTypes: attachedAttrTypes(b.attached)}
	}
	return out
}

// settingsAttrTypes is the type of the whole `settings` object.
func settingsAttrTypes() map[string]attr.Type {
	out := make(map[string]attr.Type, len(settingsBranches))
	for _, b := range settingsBranches {
		out[b.name] = types.ObjectType{AttrTypes: branchAttrTypes(b)}
	}
	return out
}

func fieldAttribute(f settingField) schema.Attribute {
	common := func() (bool, bool) { // optional, computed
		if f.serverOwned {
			return false, true
		}
		return true, true
	}
	optional, computed := common()

	switch f.kind {
	case kindBool:
		return schema.BoolAttribute{Optional: optional, Computed: computed, Sensitive: f.sensitive, Description: f.desc}
	case kindInt:
		return schema.Int64Attribute{Optional: optional, Computed: computed, Sensitive: f.sensitive, Description: f.desc}
	case kindStringSet:
		return schema.SetAttribute{ElementType: types.StringType, Optional: optional, Computed: computed, Sensitive: f.sensitive, Description: f.desc}
	case kindIntSet:
		return schema.SetAttribute{ElementType: types.Int64Type, Optional: optional, Computed: computed, Sensitive: f.sensitive, Description: f.desc}
	case kindHeaders:
		return schema.ListNestedAttribute{
			Optional:    optional,
			Computed:    computed,
			Description: f.desc,
			NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"name":  schema.StringAttribute{Required: true, Description: "The header name."},
					"value": schema.StringAttribute{Required: true, Sensitive: true, Description: "The header value. Marked sensitive: request headers routinely carry credentials."},
				},
			},
		}
	default:
		return schema.StringAttribute{Optional: optional, Computed: computed, Sensitive: f.sensitive, Description: f.desc}
	}
}

func fieldsSchema(fields []settingField) map[string]schema.Attribute {
	out := make(map[string]schema.Attribute, len(fields))
	for _, f := range fields {
		out[f.name] = fieldAttribute(f)
	}
	return out
}

func attachedSchema(kinds []string) schema.Attribute {
	attrs := make(map[string]schema.Attribute, len(kinds))
	for _, k := range kinds {
		kind := attachedKinds[k]
		attrs[k] = schema.SingleNestedAttribute{
			Optional:    true,
			Computed:    true,
			Description: kind.desc,
			Attributes:  fieldsSchema(kind.fields),
		}
	}
	return schema.SingleNestedAttribute{
		Optional:    true,
		Computed:    true,
		Description: "The sub-checks that run alongside this monitor's own check.",
		Attributes:  attrs,
	}
}

// settingsSchema builds the `settings` attribute: one nested block per
// monitor type, of which a monitor uses the one its `type` names.
func settingsSchema() schema.Attribute {
	branches := make(map[string]schema.Attribute, len(settingsBranches))
	for _, b := range settingsBranches {
		attrs := fieldsSchema(b.fields)
		if len(b.attached) > 0 {
			attrs["attached"] = attachedSchema(b.attached)
		}
		branches[b.name] = schema.SingleNestedAttribute{
			Optional:    true,
			Computed:    true,
			Description: b.desc,
			Attributes:  attrs,
		}
	}
	return schema.SingleNestedAttribute{
		Optional: true,
		Computed: true,
		Description: "The type-specific settings, as one block per monitor type. Fill in the block that matches `type` " +
			"and leave the rest out. A type without a block of its own is written through `settings_json`.",
		Attributes: branches,
	}
}

// encodeSettings turns the `settings` object into the wire object for the
// monitor's own type. It answers nil when the configuration says nothing.
func encodeSettings(ctx context.Context, settings types.Object, monitorType string) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	if settings.IsNull() || settings.IsUnknown() {
		return nil, diags
	}
	branch, ok := branchForType(monitorType)
	if !ok {
		return nil, diags
	}
	member, ok := settings.Attributes()[branch.name]
	if !ok {
		return nil, diags
	}
	obj, ok := member.(types.Object)
	if !ok || obj.IsNull() || obj.IsUnknown() {
		return nil, diags
	}

	out := encodeFields(ctx, obj, branch.fields, &diags)
	if len(branch.attached) > 0 {
		if attachedValue, ok := obj.Attributes()["attached"]; ok {
			if attachedObj, ok := attachedValue.(types.Object); ok && !attachedObj.IsNull() && !attachedObj.IsUnknown() {
				wire := make(map[string]any, len(branch.attached))
				for _, k := range branch.attached {
					kindValue, ok := attachedObj.Attributes()[k]
					if !ok {
						continue
					}
					kindObj, ok := kindValue.(types.Object)
					if !ok || kindObj.IsNull() || kindObj.IsUnknown() {
						continue
					}
					wire[attachedKinds[k].wire] = encodeFields(ctx, kindObj, attachedKinds[k].fields, &diags)
				}
				if len(wire) > 0 {
					out["attached"] = wire
				}
			}
		}
	}
	return out, diags
}

func encodeFields(ctx context.Context, obj types.Object, fields []settingField, diags *diag.Diagnostics) map[string]any {
	out := make(map[string]any, len(fields))
	attrs := obj.Attributes()
	for _, f := range fields {
		if f.serverOwned {
			continue
		}
		value, ok := attrs[f.name]
		if !ok || value.IsNull() || value.IsUnknown() {
			continue
		}
		switch f.kind {
		case kindBool:
			out[f.wire] = value.(types.Bool).ValueBool()
		case kindInt:
			out[f.wire] = value.(types.Int64).ValueInt64()
		case kindStringSet:
			var items []string
			diags.Append(value.(types.Set).ElementsAs(ctx, &items, false)...)
			out[f.wire] = items
		case kindIntSet:
			var items []int64
			diags.Append(value.(types.Set).ElementsAs(ctx, &items, false)...)
			out[f.wire] = items
		case kindHeaders:
			var headers []headerModel
			diags.Append(value.(types.List).ElementsAs(ctx, &headers, false)...)
			rows := make([]any, 0, len(headers))
			for _, h := range headers {
				rows = append(rows, map[string]any{"name": h.Name.ValueString(), "value": h.Value.ValueString()})
			}
			out[f.wire] = rows
		default:
			out[f.wire] = value.(types.String).ValueString()
		}
	}
	return out
}

type headerModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// decodeSettings builds the `settings` object from what the API returned.
//
// prior is the object the plan or the previous state carried. It matters
// for two kinds of member: one the API accepts and never publishes, whose
// only record is the configuration, and one the API leaves out of a read
// while it holds its default, which would otherwise read back as a removal
// of a value that was applied successfully.
func decodeSettings(ctx context.Context, prior types.Object, wire map[string]any, monitorType string) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	attrTypes := settingsAttrTypes()

	branch, ok := branchForType(monitorType)
	if !ok {
		return types.ObjectNull(attrTypes), diags
	}

	priorBranch := types.ObjectNull(branchAttrTypes(branch))
	if !prior.IsNull() && !prior.IsUnknown() {
		if member, ok := prior.Attributes()[branch.name]; ok {
			if obj, ok := member.(types.Object); ok {
				priorBranch = obj
			}
		}
	}
	if priorBranch.IsUnknown() {
		priorBranch = types.ObjectNull(branchAttrTypes(branch))
	}

	values := decodeFields(ctx, priorBranch, branch.fields, wire, &diags)

	if len(branch.attached) > 0 {
		attachedTypes := attachedAttrTypes(branch.attached)
		wireAttached, _ := wire["attached"].(map[string]any)
		priorAttached := types.ObjectNull(attachedTypes)
		if !priorBranch.IsNull() {
			if member, ok := priorBranch.Attributes()["attached"]; ok {
				if obj, ok := member.(types.Object); ok && !obj.IsUnknown() {
					priorAttached = obj
				}
			}
		}
		kindValues := make(map[string]attr.Value, len(branch.attached))
		anySet := false
		for _, k := range branch.attached {
			kind := attachedKinds[k]
			kindTypes := fieldsAttrTypes(kind.fields)
			priorKind := types.ObjectNull(kindTypes)
			if !priorAttached.IsNull() {
				if member, ok := priorAttached.Attributes()[k]; ok {
					if obj, ok := member.(types.Object); ok && !obj.IsUnknown() {
						priorKind = obj
					}
				}
			}
			kindWire, present := wireAttached[kind.wire].(map[string]any)
			if !present && priorKind.IsNull() {
				kindValues[k] = types.ObjectNull(kindTypes)
				continue
			}
			anySet = true
			fieldValues := decodeFields(ctx, priorKind, kind.fields, kindWire, &diags)
			obj, objDiags := types.ObjectValue(kindTypes, fieldValues)
			diags.Append(objDiags...)
			kindValues[k] = obj
		}
		if anySet {
			obj, objDiags := types.ObjectValue(attachedTypes, kindValues)
			diags.Append(objDiags...)
			values["attached"] = obj
		} else {
			values["attached"] = types.ObjectNull(attachedTypes)
		}
	}

	branchObject, branchDiags := types.ObjectValue(branchAttrTypes(branch), values)
	diags.Append(branchDiags...)

	members := make(map[string]attr.Value, len(settingsBranches))
	for _, b := range settingsBranches {
		if b.name == branch.name {
			members[b.name] = branchObject
			continue
		}
		members[b.name] = types.ObjectNull(branchAttrTypes(b))
	}
	object, objectDiags := types.ObjectValue(attrTypes, members)
	diags.Append(objectDiags...)
	return object, diags
}

func decodeFields(ctx context.Context, prior types.Object, fields []settingField, wire map[string]any, diags *diag.Diagnostics) map[string]attr.Value {
	out := make(map[string]attr.Value, len(fields))
	priorAttrs := map[string]attr.Value{}
	if !prior.IsNull() && !prior.IsUnknown() {
		priorAttrs = prior.Attributes()
	}

	for _, f := range fields {
		priorValue, hadPrior := priorAttrs[f.name]
		if hadPrior && priorValue.IsUnknown() {
			hadPrior = false
		}
		keepPrior := hadPrior && !priorValue.IsNull()

		if f.writeOnly {
			// The API never publishes it, so the configuration is the only
			// record there is.
			out[f.name] = orNull(priorValue, hadPrior, fieldType(f))
			continue
		}

		raw, present := wire[f.wire]
		if !present || raw == nil {
			if f.defaulted && keepPrior {
				out[f.name] = priorValue
				continue
			}
			out[f.name] = nullOf(fieldType(f))
			continue
		}
		out[f.name] = decodeValue(ctx, f, raw, diags)
	}
	return out
}

func decodeValue(ctx context.Context, f settingField, raw any, diags *diag.Diagnostics) attr.Value {
	switch f.kind {
	case kindBool:
		switch v := raw.(type) {
		case bool:
			return types.BoolValue(v)
		case float64:
			return types.BoolValue(v != 0)
		}
		return types.BoolNull()
	case kindInt:
		if v, ok := toInt64(raw); ok {
			return types.Int64Value(v)
		}
		return types.Int64Null()
	case kindStringSet:
		items, ok := raw.([]any)
		if !ok {
			return types.SetNull(types.StringType)
		}
		elements := make([]attr.Value, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				elements = append(elements, types.StringValue(s))
			}
		}
		set, setDiags := types.SetValue(types.StringType, elements)
		diags.Append(setDiags...)
		return set
	case kindIntSet:
		items, ok := raw.([]any)
		if !ok {
			return types.SetNull(types.Int64Type)
		}
		elements := make([]attr.Value, 0, len(items))
		for _, item := range items {
			if v, ok := toInt64(item); ok {
				elements = append(elements, types.Int64Value(v))
			}
		}
		set, setDiags := types.SetValue(types.Int64Type, elements)
		diags.Append(setDiags...)
		return set
	case kindHeaders:
		items, ok := raw.([]any)
		if !ok {
			return types.ListNull(types.ObjectType{AttrTypes: headerAttrTypes})
		}
		elements := make([]attr.Value, 0, len(items))
		for _, item := range items {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := row["name"].(string)
			value, _ := row["value"].(string)
			obj, objDiags := types.ObjectValue(headerAttrTypes, map[string]attr.Value{
				"name":  types.StringValue(name),
				"value": types.StringValue(value),
			})
			diags.Append(objDiags...)
			elements = append(elements, obj)
		}
		list, listDiags := types.ListValue(types.ObjectType{AttrTypes: headerAttrTypes}, elements)
		diags.Append(listDiags...)
		return list
	default:
		if s, ok := raw.(string); ok {
			return types.StringValue(s)
		}
		return types.StringNull()
	}
}

func orNull(value attr.Value, present bool, t attr.Type) attr.Value {
	if present && value != nil {
		return value
	}
	return nullOf(t)
}

func nullOf(t attr.Type) attr.Value {
	switch typed := t.(type) {
	case types.SetType:
		return types.SetNull(typed.ElemType)
	case types.ListType:
		return types.ListNull(typed.ElemType)
	case types.ObjectType:
		return types.ObjectNull(typed.AttrTypes)
	}
	switch t {
	case types.BoolType:
		return types.BoolNull()
	case types.Int64Type:
		return types.Int64Null()
	default:
		return types.StringNull()
	}
}

func toInt64(raw any) (int64, bool) {
	switch v := raw.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	}
	return 0, false
}
