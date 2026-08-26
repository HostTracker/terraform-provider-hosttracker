package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*contactGroupResource)(nil)
	_ resource.ResourceWithConfigure   = (*contactGroupResource)(nil)
	_ resource.ResourceWithImportState = (*contactGroupResource)(nil)
)

// NewContactGroupResource is the hosttracker_contact_group constructor.
func NewContactGroupResource() resource.Resource { return &contactGroupResource{} }

type contactGroupResource struct {
	api *client.Client
}

type contactGroupModel struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	Items   types.Set    `tfsdk:"items"`
	Created types.Int64  `tfsdk:"created"`
}

var contactGroupItemAttrTypes = map[string]attr.Type{
	"contact_id": types.StringType,
	"events":     types.SetType{ElemType: types.StringType},
}

// groupEventVocabulary is what a membership may subscribe to. It spans
// both subscription kinds, because one preset row can carry either.
const groupEventVocabulary = "`up`, `down`, `repeatedlyDown`, `daily`, `weekly`, `monthly`, `quarterly` or `yearly`"

func (r *contactGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact_group"
}

func (r *contactGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A contact group: a named preset of contacts and the events each of them is subscribed to " +
			"by it.\n\n" +
			"A group is passive. Membership by itself delivers nothing - the group is applied when a monitor is " +
			"created or edited with it, and what it carries is the event list below.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The group's id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The group's name, unique per account and matched case-insensitively.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
			"items": schema.SetNestedAttribute{
				Required: true,
				Description: "The group's whole membership. Writing it replaces the membership rather than " +
					"adding to it, because a group is a snapshot.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"contact_id": schema.StringAttribute{
							Required:    true,
							Description: "The contact, which must be the account's own.",
							Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
						},
						"events": schema.SetAttribute{
							ElementType: types.StringType,
							Required:    true,
							Description: "What this membership subscribes the contact to - at least one of " +
								groupEventVocabulary + ". The vocabulary is open, so an event newer than this " +
								"provider is accepted too.",
							Validators: []validator.Set{setvalidator.SizeAtLeast(1)},
						},
					},
				},
			},
			"created": schema.Int64Attribute{
				Computed:    true,
				Description: "When the group was created, in Unix seconds.",
			},
		},
	}
}

func (r *contactGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("The contact group resource was configured with %T instead of a *client.Client. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.api = api
}

func (r *contactGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan contactGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := encodeContactGroup(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.api.CreateContactGroup(ctx, body)
	if err != nil {
		resp.Diagnostics.Append(client.Diagnose("create the contact group", err, contactGroupPointerMapper)...)
		return
	}

	state, diags := contactGroupToState(ctx, group)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *contactGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state contactGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.api.GetContactGroup(ctx, state.ID.ValueString())
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("read the contact group", err, contactGroupPointerMapper)...)
		return
	}

	refreshed, diags := contactGroupToState(ctx, group)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

func (r *contactGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state contactGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	want, diags := encodeContactGroup(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	have, diags := encodeContactGroup(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.api.UpdateContactGroup(ctx, state.ID.ValueString(), client.Diff(have, want))
	if err != nil {
		if err == client.ErrNotFound || client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			resp.Diagnostics.AddError(
				"The contact group is gone",
				"It was deleted outside Terraform between the plan and the apply. Run the apply again to create it.",
			)
			return
		}
		resp.Diagnostics.Append(client.Diagnose("update the contact group", err, contactGroupPointerMapper)...)
		return
	}

	updated, diags := contactGroupToState(ctx, group)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &updated)...)
}

func (r *contactGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state contactGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.api.DeleteContactGroup(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.Append(client.Diagnose("delete the contact group", err, nil)...)
	}
}

func (r *contactGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// contactGroupPointerMapper places a problem document's JSON Pointers on
// the attributes a configuration wrote. A pointer into the membership
// names the whole set: the API's index is over the array it received, and
// a set has no index the configuration would recognise.
func contactGroupPointerMapper(pointer string) (path.Path, bool) {
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	if len(parts) == 0 {
		return path.Empty(), false
	}
	switch parts[0] {
	case "name":
		return path.Root("name"), true
	case "items":
		return path.Root("items"), true
	}
	return path.Empty(), false
}

// encodeContactGroup turns the model into a wire body, with the membership
// in a stable order so that two equal memberships compare equal.
func encodeContactGroup(ctx context.Context, m *contactGroupModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{}

	putString(body, "name", m.Name)

	if !m.Items.IsNull() && !m.Items.IsUnknown() {
		entries := make([]map[string]any, 0, len(m.Items.Elements()))
		for _, element := range m.Items.Elements() {
			object, ok := element.(types.Object)
			if !ok || object.IsNull() || object.IsUnknown() {
				continue
			}
			attrs := object.Attributes()
			entry := map[string]any{}
			if id, ok := attrs["contact_id"].(types.String); ok && known(id) {
				entry["contact"] = id.ValueString()
			}
			if events, ok := attrs["events"].(types.Set); ok && !events.IsNull() && !events.IsUnknown() {
				var values []string
				diags.Append(events.ElementsAs(ctx, &values, false)...)
				sort.Strings(values)
				entry["events"] = values
			}
			entries = append(entries, entry)
		}
		sort.Slice(entries, func(i, j int) bool {
			left, _ := entries[i]["contact"].(string)
			right, _ := entries[j]["contact"].(string)
			return left < right
		})
		items := make([]any, 0, len(entries))
		for _, entry := range entries {
			items = append(items, entry)
		}
		body["items"] = items
	}

	return body, diags
}

// contactGroupToState renders what the API returned as resource state. A
// read carries each member's identifying projection where a write carried
// its id; only the id is kept, because that is what the configuration owns.
func contactGroupToState(ctx context.Context, g *client.ContactGroup) (contactGroupModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	state := contactGroupModel{
		ID:      types.StringValue(g.ID),
		Name:    stringOrNull(g.Name),
		Created: types.Int64Value(g.Created),
	}

	elemType := types.ObjectType{AttrTypes: contactGroupItemAttrTypes}
	values := make([]attr.Value, 0, len(g.Items))
	for _, item := range g.Items {
		events, eventDiags := stringSet(ctx, item.Events)
		diags.Append(eventDiags...)
		object, objectDiags := types.ObjectValue(contactGroupItemAttrTypes, map[string]attr.Value{
			"contact_id": types.StringValue(item.ContactID()),
			"events":     events,
		})
		diags.Append(objectDiags...)
		values = append(values, object)
	}
	set, setDiags := types.SetValue(elemType, values)
	diags.Append(setDiags...)
	state.Items = set

	return state, diags
}
