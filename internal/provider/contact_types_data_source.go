package provider

import (
	"context"
	"fmt"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*contactTypesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*contactTypesDataSource)(nil)
)

// NewContactTypesDataSource is the hosttracker_contact_types constructor.
func NewContactTypesDataSource() datasource.DataSource { return &contactTypesDataSource{} }

type contactTypesDataSource struct {
	api *client.Client
}

type contactTypesModel struct {
	Types types.List `tfsdk:"types"`
}

var templateParameterAttrTypes = map[string]attr.Type{
	"name":        types.StringType,
	"description": types.StringType,
	"events":      types.ListType{ElemType: types.StringType},
}

var contactTypeAttrTypes = map[string]attr.Type{
	"type":                  types.StringType,
	"label":                 types.StringType,
	"creatable":             types.BoolType,
	"confirmable":           types.BoolType,
	"requires_registration": types.BoolType,
	"supports_reports":      types.BoolType,
	"gateways":              types.ListType{ElemType: types.StringType},
	"alert_delays":          types.ListType{ElemType: types.Int64Type},
	"template_parameters":   types.ListType{ElemType: types.ObjectType{AttrTypes: templateParameterAttrTypes}},
}

func (d *contactTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact_types"
}

func (d *contactTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "The contact-type catalogue: what each channel is called, whether it can be created " +
			"through the API at all, whether it needs confirming, and which alert delays it accepts.",
		Attributes: map[string]dschema.Attribute{
			"types": dschema.ListNestedAttribute{
				Computed:    true,
				Description: "One row per contact type.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"type":  dschema.StringAttribute{Computed: true, Description: "The type token, as `type` on a contact takes it."},
						"label": dschema.StringAttribute{Computed: true, Description: "The human name."},
						"creatable": dschema.BoolAttribute{
							Computed: true,
							Description: "Whether a contact of this type can be created through the API. A " +
								"messenger channel is not: the recipient registers with the bot instead.",
						},
						"confirmable": dschema.BoolAttribute{
							Computed:    true,
							Description: "Whether a contact of this type must confirm a code before it receives anything.",
						},
						"requires_registration": dschema.BoolAttribute{
							Computed:    true,
							Description: "Whether the recipient must register with a messenger before delivery works.",
						},
						"supports_reports": dschema.BoolAttribute{
							Computed:    true,
							Description: "Whether scheduled reports can be delivered to this type.",
						},
						"gateways": dschema.ListAttribute{
							ElementType: types.StringType,
							Computed:    true,
							Description: "The delivery gateways this type can be routed through.",
						},
						"alert_delays": dschema.ListAttribute{
							ElementType: types.Int64Type,
							Computed:    true,
							Description: "The alert delays this type accepts, in minutes. A contact's `alert_delay` must be one of them.",
						},
						"template_parameters": dschema.ListNestedAttribute{
							Computed: true,
							Description: "The `[[token]]` vocabulary a custom message body may use. On the `http` " +
								"row only.",
							NestedObject: dschema.NestedAttributeObject{
								Attributes: map[string]dschema.Attribute{
									"name":        dschema.StringAttribute{Computed: true, Description: "The token's name, written `[[name]]` in a body."},
									"description": dschema.StringAttribute{Computed: true, Description: "What the renderer substitutes for it."},
									"events": dschema.ListAttribute{
										ElementType: types.StringType,
										Computed:    true,
										Description: "The alert types the token resolves on; the others render it blank.",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *contactTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The contact types data source was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	d.api = api
}

func (d *contactTypesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	catalogue, err := d.api.ListContactTypes(ctx)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("read the contact-type catalogue", err, nil)...)
		return
	}

	rows := make([]attr.Value, 0, len(catalogue))
	for _, item := range catalogue {
		gateways, gatewayDiags := stringList(ctx, item.Gateways)
		resp.Diagnostics.Append(gatewayDiags...)
		delays, delayDiags := int64List(ctx, item.AlertDelays)
		resp.Diagnostics.Append(delayDiags...)

		parameters := types.ListNull(types.ObjectType{AttrTypes: templateParameterAttrTypes})
		if item.TemplateParameters != nil {
			values := make([]attr.Value, 0, len(*item.TemplateParameters))
			for _, parameter := range *item.TemplateParameters {
				events, eventDiags := stringList(ctx, parameter.Events)
				resp.Diagnostics.Append(eventDiags...)
				object, objectDiags := types.ObjectValue(templateParameterAttrTypes, map[string]attr.Value{
					"name":        types.StringPointerValue(parameter.Name),
					"description": types.StringPointerValue(parameter.Description),
					"events":      events,
				})
				resp.Diagnostics.Append(objectDiags...)
				values = append(values, object)
			}
			list, listDiags := types.ListValue(types.ObjectType{AttrTypes: templateParameterAttrTypes}, values)
			resp.Diagnostics.Append(listDiags...)
			parameters = list
		}

		object, objectDiags := types.ObjectValue(contactTypeAttrTypes, map[string]attr.Value{
			"type":                  types.StringPointerValue(item.Type),
			"label":                 types.StringPointerValue(item.Label),
			"creatable":             types.BoolPointerValue(item.Creatable),
			"confirmable":           types.BoolPointerValue(item.Confirmable),
			"requires_registration": types.BoolPointerValue(item.RequiresRegistration),
			"supports_reports":      types.BoolPointerValue(item.SupportsReports),
			"gateways":              gateways,
			"alert_delays":          delays,
			"template_parameters":   parameters,
		})
		resp.Diagnostics.Append(objectDiags...)
		rows = append(rows, object)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	list, diags := types.ListValue(types.ObjectType{AttrTypes: contactTypeAttrTypes}, rows)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &contactTypesModel{Types: list})...)
}

func int64List(ctx context.Context, values *[]int64) (types.List, diag.Diagnostics) {
	if values == nil {
		return types.ListNull(types.Int64Type), nil
	}
	return types.ListValueFrom(ctx, types.Int64Type, *values)
}
