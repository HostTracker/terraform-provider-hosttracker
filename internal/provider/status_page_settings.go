package provider

import (
	"context"
	"regexp"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// settingKind is how one settings member travels: it decides both the
// Terraform type and how the member is read out of a decoded answer.
type settingKind int

const (
	settingString settingKind = iota
	settingBool
	settingFloat
	settingStringSet
)

// statusPageSettingField pairs a Terraform attribute with its wire member.
// The table drives the two schemas, the encoder and the decoder, so a new
// member is one row rather than four edits.
type statusPageSettingField struct {
	name string
	wire string
	kind settingKind
	desc string
	// validators applies to the resource schema only: a data source reads
	// what is stored and has nothing to refuse.
	validators []validator.String
}

// statusPageFeatures is the whole feature vocabulary. The set is closed:
// sending the array replaces it, and a feature not listed is off.
var statusPageFeatures = []string{
	"barCharts", "uptimePercent", "outageDetails", "detailsPages", "floatingBar",
	"monitorUrls", "hidePaused", "overallUptime", "downtimeFeed", "subscribe",
}

// statusPageLanguages is what the public page can be rendered in.
var statusPageLanguages = []string{
	"en", "cs", "de", "es", "fr", "it", "ja", "nl", "pl", "pt", "ru", "tr", "ua", "zh",
}

// hexColorPattern is how the page's colours are spelled.
var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func hexColorValidators() []validator.String {
	return []validator.String{stringvalidator.RegexMatches(hexColorPattern, "must be spelled `#rrggbb`")}
}

var statusPageSettingsFields = []statusPageSettingField{
	{name: "homepage_url", wire: "homepageUrl", kind: settingString,
		desc: "Where the page's logo and title link to."},
	{name: "logo_url", wire: "logoUrl", kind: settingString,
		desc: "The logo shown at the top of the page."},
	{name: "dark_logo_url", wire: "darkLogoUrl", kind: settingString,
		desc: "The logo used when the page renders dark."},
	{name: "favicon_url", wire: "faviconUrl", kind: settingString,
		desc: "The icon browsers show for the page."},
	{name: "theme", wire: "theme", kind: settingString,
		desc:       "Which palette the page renders in: `light` or `dark`.",
		validators: []validator.String{stringvalidator.OneOf("light", "dark")}},
	{name: "theme_color", wire: "themeColor", kind: settingString,
		desc: "The page's accent colour, spelled `#rrggbb`.", validators: hexColorValidators()},
	{name: "header_bg_color", wire: "headerBgColor", kind: settingString,
		desc: "The header's background colour, spelled `#rrggbb`.", validators: hexColorValidators()},
	{name: "header_text_color", wire: "headerTextColor", kind: settingString,
		desc: "The header's text colour, spelled `#rrggbb`.", validators: hexColorValidators()},
	{name: "announcement", wire: "announcement", kind: settingString,
		desc:       "A banner shown above the components, at most 500 characters. Write `\"\"` to remove it.",
		validators: []validator.String{stringvalidator.LengthAtMost(500)}},
	{name: "density", wire: "density", kind: settingString,
		desc:       "How tightly the component rows are packed: `wide` or `compact`.",
		validators: []validator.String{stringvalidator.OneOf("wide", "compact")}},
	{name: "logo_alignment", wire: "logoAlignment", kind: settingString,
		desc:       "Where the logo sits in the header: `left` or `center`.",
		validators: []validator.String{stringvalidator.OneOf("left", "center")}},
	{name: "show_groups", wire: "showGroups", kind: settingBool,
		desc: "Render components under their group headings."},
	{name: "robots_index", wire: "robotsIndex", kind: settingBool,
		desc: "Allow search engines to index the public page."},
	{name: "language", wire: "language", kind: settingString,
		desc:       "The language the public page is rendered in: " + backquotedList(statusPageLanguages) + ".",
		validators: []validator.String{stringvalidator.OneOf(statusPageLanguages...)}},
	{name: "google_analytics_id", wire: "googleAnalyticsId", kind: settingString,
		desc: "A Google Analytics measurement id (`G-XXXXXXXX`)."},
	{name: "hide_branding", wire: "hideBranding", kind: settingBool,
		desc: "Hide the HostTracker attribution, where the plan allows it."},
	{name: "auto_add_monitors", wire: "autoAddMonitors", kind: settingBool,
		desc: "Add newly created monitors to this page automatically. Leave it off when Terraform owns " +
			"`components`: a monitor the API adds by itself is a component the next plan proposes to remove."},
	{name: "features", wire: "features", kind: settingStringSet,
		desc: "The page's WHOLE feature set - writing it replaces what is there, and a feature not listed is off."},
	{name: "sla_target", wire: "slaTarget", kind: settingFloat,
		desc: "The uptime percentage the page measures its components against, between 0 and 100. Leave it out " +
			"for each monitor's own target."},
}

func statusPageSettingsAttrTypes() map[string]attr.Type {
	out := make(map[string]attr.Type, len(statusPageSettingsFields))
	for _, f := range statusPageSettingsFields {
		switch f.kind {
		case settingBool:
			out[f.name] = types.BoolType
		case settingFloat:
			out[f.name] = types.Float64Type
		case settingStringSet:
			out[f.name] = types.SetType{ElemType: types.StringType}
		default:
			out[f.name] = types.StringType
		}
	}
	return out
}

func statusPageSettingsSchema() schema.SingleNestedAttribute {
	attributes := make(map[string]schema.Attribute, len(statusPageSettingsFields))
	for _, f := range statusPageSettingsFields {
		description := f.desc
		switch f.kind {
		case settingBool:
			attributes[f.name] = schema.BoolAttribute{Optional: true, Computed: true, Description: description}
		case settingFloat:
			attributes[f.name] = schema.Float64Attribute{
				Optional:    true,
				Computed:    true,
				Description: description,
				Validators:  []validator.Float64{float64validator.Between(0, 100)},
			}
		case settingStringSet:
			attributes[f.name] = schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: description + " One of " + backquotedList(statusPageFeatures) + ".",
			}
		default:
			attributes[f.name] = schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: description,
				Validators:  f.validators,
			}
		}
	}

	return schema.SingleNestedAttribute{
		Optional: true,
		Computed: true,
		Description: "How the public page looks and behaves. Every member is optional and also computed: a member " +
			"the configuration leaves out keeps the value it has, because the API reads an absent member as " +
			"\"leave this alone\".",
		Attributes: attributes,
	}
}

func statusPageSettingsDataSourceSchema() dschema.SingleNestedAttribute {
	attributes := make(map[string]dschema.Attribute, len(statusPageSettingsFields))
	for _, f := range statusPageSettingsFields {
		switch f.kind {
		case settingBool:
			attributes[f.name] = dschema.BoolAttribute{Computed: true, Description: f.desc}
		case settingFloat:
			attributes[f.name] = dschema.Float64Attribute{Computed: true, Description: f.desc}
		case settingStringSet:
			attributes[f.name] = dschema.SetAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: f.desc,
			}
		default:
			attributes[f.name] = dschema.StringAttribute{Computed: true, Description: f.desc}
		}
	}
	return dschema.SingleNestedAttribute{
		Computed:    true,
		Description: "How the public page looks and behaves.",
		Attributes:  attributes,
	}
}

// encodeStatusPageSettings turns the settings object into a wire map,
// leaving out the members the configuration says nothing about.
func encodeStatusPageSettings(ctx context.Context, settings types.Object) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	if settings.IsNull() || settings.IsUnknown() {
		return nil, diags
	}

	body := map[string]any{}
	attrs := settings.Attributes()
	for _, f := range statusPageSettingsFields {
		value, ok := attrs[f.name]
		if !ok || value.IsNull() || value.IsUnknown() {
			continue
		}
		switch f.kind {
		case settingBool:
			if v, ok := value.(types.Bool); ok {
				body[f.wire] = v.ValueBool()
			}
		case settingFloat:
			if v, ok := value.(types.Float64); ok {
				body[f.wire] = v.ValueFloat64()
			}
		case settingStringSet:
			if v, ok := value.(types.Set); ok {
				var values []string
				diags.Append(v.ElementsAs(ctx, &values, false)...)
				sort.Strings(values)
				body[f.wire] = values
			}
		default:
			if v, ok := value.(types.String); ok {
				body[f.wire] = v.ValueString()
			}
		}
	}
	return body, diags
}

// decodeStatusPageSettings renders the stored settings as an object. A
// member the answer does not carry reads null.
func decodeStatusPageSettings(ctx context.Context, stored map[string]any) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	attrTypes := statusPageSettingsAttrTypes()
	if stored == nil {
		return types.ObjectNull(attrTypes), diags
	}

	values := make(map[string]attr.Value, len(statusPageSettingsFields))
	for _, f := range statusPageSettingsFields {
		raw, present := stored[f.wire]
		switch f.kind {
		case settingBool:
			if v, ok := raw.(bool); present && ok {
				values[f.name] = types.BoolValue(v)
			} else {
				values[f.name] = types.BoolNull()
			}
		case settingFloat:
			if v, ok := raw.(float64); present && ok {
				values[f.name] = types.Float64Value(v)
			} else {
				values[f.name] = types.Float64Null()
			}
		case settingStringSet:
			list, ok := raw.([]any)
			if !present || !ok {
				values[f.name] = types.SetNull(types.StringType)
				continue
			}
			items := make([]string, 0, len(list))
			for _, item := range list {
				if s, ok := item.(string); ok {
					items = append(items, s)
				}
			}
			sort.Strings(items)
			set, setDiags := types.SetValueFrom(ctx, types.StringType, items)
			diags.Append(setDiags...)
			values[f.name] = set
		default:
			if v, ok := raw.(string); present && ok {
				values[f.name] = types.StringValue(v)
			} else {
				values[f.name] = types.StringNull()
			}
		}
	}

	object, objectDiags := types.ObjectValue(attrTypes, values)
	diags.Append(objectDiags...)
	return object, diags
}
