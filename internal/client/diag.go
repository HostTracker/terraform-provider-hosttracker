package client

import (
	"fmt"
	"sort"
	"strings"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// PointerMapper turns a problem document's JSON Pointer into the attribute
// path the practitioner wrote, so a 422 lands on the offending line of the
// configuration instead of on the resource as a whole. It reports false for
// a pointer that has no configuration attribute behind it.
type PointerMapper func(pointer string) (path.Path, bool)

// Diagnose renders an error from the SDK as Terraform diagnostics. action
// names what was being done ("create the monitor"), and mapper - which may
// be nil - places per-attribute failures.
func Diagnose(action string, err error, mapper PointerMapper) diag.Diagnostics {
	var diags diag.Diagnostics
	if err == nil {
		return diags
	}

	e, ok := hosttracker.AsError(err)
	if !ok {
		diags.AddError(fmt.Sprintf("Could not %s", action), err.Error())
		return diags
	}

	switch e.Code {
	case hosttracker.CodeNetworkError:
		diags.AddError(
			fmt.Sprintf("Could not %s", action),
			fmt.Sprintf("The API at %s could not be reached: %s.", hostOf(e.URL), rootCause(e)),
		)
		return diags

	case hosttracker.CodeInvalidToken:
		diags.AddError(
			"The API rejected the token",
			join(
				detailOf(e),
				"Set a valid token on the provider block, or in the HT_TOKEN environment variable. Tokens are minted at https://www.host-tracker.com/integrations/api.",
				requestSuffix(e),
			),
		)
		return diags

	case hosttracker.CodeMissingScope:
		diags.AddError(
			fmt.Sprintf("The token cannot %s", action),
			join(detailOf(e), scopeHint(e), requestSuffix(e)),
		)
		return diags

	case "package_limit":
		diags.AddError(
			"The account's package does not allow this",
			join(detailOf(e), featureHint(e), "Upgrade the package, or remove monitors to free the allowance.", requestSuffix(e)),
		)
		return diags

	case "duplicate_monitor":
		if existing, ok := ExistingID(err); ok {
			diags.AddError(
				"A monitor with this address already exists",
				join(
					detailOf(e),
					fmt.Sprintf("Import it instead of creating a second one:\n\n    terraform import hosttracker_monitor.<name> %s", existing),
					requestSuffix(e),
				),
			)
			return diags
		}

	case hosttracker.CodeQuotaExceeded:
		diags.AddError(
			"The account's API quota is spent",
			join(detailOf(e), quotaHint(e), requestSuffix(e)),
		)
		return diags

	case hosttracker.CodeNotFound:
		diags.AddError(
			fmt.Sprintf("Could not %s: the resource does not exist", action),
			join(detailOf(e), requestSuffix(e)),
		)
		return diags
	}

	// Validation failures carry one entry per offending value. Each entry
	// that maps to an attribute becomes a diagnostic on that attribute.
	placed := 0
	if e.Status == 422 && len(e.Errors) > 0 && mapper != nil {
		for _, item := range e.Errors {
			pointer, _ := item["pointer"].(string)
			if pointer == "" {
				if p, ok := item["parameter"].(string); ok && p != "" {
					pointer = "/" + p
				}
			}
			if pointer == "" {
				continue
			}
			attr, ok := mapper(pointer)
			if !ok {
				continue
			}
			diags.AddAttributeError(attr, "The API refused this value", itemDetail(e, item))
			placed++
		}
	}
	if placed == len(e.Errors) && placed > 0 {
		return diags
	}

	detail := join(detailOf(e), unplacedItems(e, placed), requestSuffix(e))
	diags.AddError(fmt.Sprintf("Could not %s", action), detail)
	return diags
}

// IsNotFound reports the code a read answers for a resource that is gone,
// or was never this account's.
func IsNotFound(err error) bool {
	return hosttracker.IsNotFound(err)
}

// ExistingID reads the id a 409 duplicate names, which is the id to import.
func ExistingID(err error) (string, bool) {
	e, ok := hosttracker.AsError(err)
	if !ok {
		return "", false
	}
	for _, item := range e.Errors {
		if id, ok := item["existingId"].(string); ok && id != "" {
			return id, true
		}
	}
	return "", false
}

// PackageFeature reads the entitlement a 403 package_limit named.
func PackageFeature(err error) (string, bool) {
	e, ok := hosttracker.AsError(err)
	if !ok || e.Code != "package_limit" {
		return "", false
	}
	for _, item := range e.Errors {
		if f, ok := item["feature"].(string); ok && f != "" {
			return f, true
		}
	}
	return "", false
}

func detailOf(e *hosttracker.Error) string {
	switch {
	case e.Detail != "":
		return e.Detail
	case e.Title != "":
		return e.Title
	default:
		return fmt.Sprintf("The API answered %d %s.", e.Status, e.Code)
	}
}

func itemDetail(e *hosttracker.Error, item map[string]any) string {
	parts := []string{detailOf(e)}
	if reason, ok := item["reason"].(string); ok && reason != "" {
		parts = append(parts, "Reason: "+reason+".")
	}
	if allowed, ok := item["allowed"]; ok {
		parts = append(parts, "Allowed: "+render(allowed)+".")
	}
	if suggestion, ok := item["didYouMean"]; ok {
		parts = append(parts, "Did you mean "+render(suggestion)+"?")
	}
	if lo, ok := item["min"]; ok {
		if hi, ok := item["max"]; ok {
			parts = append(parts, fmt.Sprintf("Allowed range: %s to %s.", render(lo), render(hi)))
		} else {
			parts = append(parts, "Minimum: "+render(lo)+".")
		}
	} else if hi, ok := item["max"]; ok {
		parts = append(parts, "Maximum: "+render(hi)+".")
	}
	parts = append(parts, requestSuffix(e))
	return join(parts...)
}

func unplacedItems(e *hosttracker.Error, placed int) string {
	if len(e.Errors) == 0 || placed == len(e.Errors) {
		return ""
	}
	lines := make([]string, 0, len(e.Errors))
	for _, item := range e.Errors {
		where, _ := item["pointer"].(string)
		if where == "" {
			if p, ok := item["parameter"].(string); ok {
				where = "/" + p
			}
		}
		reason, _ := item["reason"].(string)
		switch {
		case where != "" && reason != "":
			lines = append(lines, fmt.Sprintf("  %s: %s", where, reason))
		case where != "":
			lines = append(lines, "  "+where+": "+render(item))
		default:
			lines = append(lines, "  "+render(item))
		}
	}
	return strings.Join(lines, "\n")
}

func scopeHint(e *hosttracker.Error) string {
	for _, item := range e.Errors {
		if required, ok := item["required"].(string); ok && required != "" {
			return fmt.Sprintf("The token needs the %q scope. Mint a new one with it at https://www.host-tracker.com/integrations/api.", required)
		}
	}
	return "Mint a token carrying the scopes this configuration needs at https://www.host-tracker.com/integrations/api."
}

func featureHint(e *hosttracker.Error) string {
	if feature, ok := PackageFeature(e); ok {
		return fmt.Sprintf("The entitlement the package is missing is %q.", feature)
	}
	return ""
}

func quotaHint(e *hosttracker.Error) string {
	for _, item := range e.Errors {
		if reset, ok := item["resetAt"]; ok {
			return fmt.Sprintf("The window resets at %s (Unix seconds).", render(reset))
		}
	}
	if e.RetryAfter > 0 {
		return fmt.Sprintf("Retry in %s.", e.RetryAfter)
	}
	return ""
}

func requestSuffix(e *hosttracker.Error) string {
	if e.RequestID == "" {
		return ""
	}
	return "Request id: " + e.RequestID + "."
}

func rootCause(e *hosttracker.Error) string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Detail
}

func hostOf(rawURL string) string {
	if rawURL == "" {
		return "the API"
	}
	if i := strings.Index(rawURL, "://"); i >= 0 {
		rest := rawURL[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return rawURL[:i+3] + rest[:j]
		}
	}
	return rawURL
}

func render(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, render(item))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+render(t[k]))
		}
		return strings.Join(parts, " ")
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func join(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}
