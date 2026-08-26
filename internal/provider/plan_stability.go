package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// This file holds what keeps a resource whose configuration writes only some
// of its members from planning an update on every run.
//
// Terraform proposes a NULL value for a nested attribute the configuration
// leaves out, even when the attribute is Computed and the state holds a value
// for it. The framework sees a proposal that differs from the state and then
// turns EVERY computed attribute whose configuration value is null into
// "known after apply" - so one omitted nested attribute is enough to make the
// whole resource read as changed, forever, with nothing for the apply to do.
//
// The cure is to answer that question ourselves: when nothing the
// configuration owns asks for something the state does not already say, the
// resource is unchanged and the state it holds is the plan.

// plannedMember pairs what a configuration wrote for one attribute with what
// the plan and the state hold for it.
type plannedMember struct {
	configured attr.Value
	planned    attr.Value
	current    attr.Value
}

// asksForChange reports whether any member carries an opinion the state does
// not already satisfy.
//
// A member the configuration leaves out carries no opinion: these APIs read an
// absent member as "leave this alone" rather than as "clear this", and it is
// exactly those members that Terraform hands us as unknown. A member the
// configuration does write is compared as planned, which is what the schema's
// defaults and any per-attribute plan modifiers have already settled.
func asksForChange(members []plannedMember) bool {
	for _, m := range members {
		if m.configured.IsNull() && m.planned.IsUnknown() {
			continue
		}
		if !m.planned.Equal(m.current) {
			return true
		}
	}
	return false
}

// keepPriorState plans no change at all, by handing back the state the
// resource already holds. It undoes the unknowns the framework marked while
// deciding, wrongly, that there was something to do.
func keepPriorState(req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	resp.Plan.Raw = req.State.Raw
}
