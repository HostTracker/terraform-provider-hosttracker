package provider

import (
	"context"
	"os"
	"time"

	hosttracker "github.com/HostTracker/hosttracker-sdk-go"
	"github.com/HostTracker/terraform-provider-hosttracker/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the framework's interfaces.
var _ provider.Provider = (*hostTrackerProvider)(nil)

// New returns the provider constructor goreleaser stamps a version into.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &hostTrackerProvider{version: version}
	}
}

type hostTrackerProvider struct {
	version string
}

type providerModel struct {
	Token    types.String `tfsdk:"token"`
	BaseURL  types.String `tfsdk:"base_url"`
	Timeout  types.Int64  `tfsdk:"timeout"`
	RetryMax types.Int64  `tfsdk:"retry_max"`
}

func (p *hostTrackerProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "hosttracker"
	resp.Version = p.version
}

func (p *hostTrackerProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages HostTracker monitoring as code through the HostTracker API v2.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "The API token. Mint one at https://www.host-tracker.com/integrations/api, " +
					"carrying the scopes this configuration needs (`monitor:read` and `monitor:write` for the " +
					"monitor resource). Falls back to the `HT_TOKEN` environment variable, which is where a " +
					"token belongs: a token in a `.tf` file ends up in version control and in the state file.",
			},
			"base_url": schema.StringAttribute{
				Optional: true,
				Description: "The API root. Defaults to `" + client.DefaultBaseURL + "`, or to the `HT_BASE_URL` " +
					"environment variable when it is set. The API version is the host, not a path segment.",
			},
			"timeout": schema.Int64Attribute{
				Optional: true,
				Description: "How long one request attempt may take, in seconds. Defaults to 30. It is applied " +
					"per attempt, on top of whatever deadline Terraform itself carries.",
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"retry_max": schema.Int64Attribute{
				Optional: true,
				Description: "How many times a throttled or retryable request is retried. Defaults to 2. " +
					"Retries honour the API's `Retry-After` and are never taken for a failure that reached " +
					"the server and was refused.",
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
		},
	}
}

func (p *hostTrackerProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"The token is not known yet",
			"The provider cannot be configured with a token that another resource produces during the same apply. "+
				"Set it from a variable, or from the HT_TOKEN environment variable.",
		)
		return
	}

	token := firstNonEmpty(config.Token.ValueString(), os.Getenv("HT_TOKEN"))
	baseURL := firstNonEmpty(config.BaseURL.ValueString(), os.Getenv("HT_BASE_URL"), client.DefaultBaseURL)

	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"No API token",
			"Set `token` on the provider block, or the HT_TOKEN environment variable. "+
				"Mint a token at https://www.host-tracker.com/integrations/api.",
		)
		return
	}

	cfg := client.Config{
		Token:     token,
		BaseURL:   baseURL,
		UserAgent: "hosttracker-terraform/" + p.version,
	}
	if !config.Timeout.IsNull() {
		cfg.Timeout = time.Duration(config.Timeout.ValueInt64()) * time.Second
	}
	if !config.RetryMax.IsNull() {
		cfg.RetryMax = int(config.RetryMax.ValueInt64())
	}

	api, err := client.New(cfg)
	if err != nil {
		resp.Diagnostics.AddError("Could not build the API client", err.Error())
		return
	}

	// One read proves the token before any resource tries to use it, so a
	// bad token fails on the provider block rather than on whichever
	// resource happened to run first. A token that is valid but carries no
	// account scope answers 403 missing_scope, which is not a reason to
	// refuse: it is a monitor-only token doing exactly what it should.
	if _, err := api.API().GetAccountWithResponse(ctx, nil); err != nil {
		switch {
		case hosttracker.IsCode(err, hosttracker.CodeMissingScope):
			// Deliberately ignored, see above.
		case hosttracker.IsCode(err, hosttracker.CodeInvalidToken):
			resp.Diagnostics.Append(client.Diagnose("authenticate against the API", err, nil)...)
			return
		default:
			resp.Diagnostics.Append(client.Diagnose("reach the API", err, nil)...)
			return
		}
	}

	resp.DataSourceData = api
	resp.ResourceData = api
}

func (p *hostTrackerProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewMonitorResource,
		NewWebhookResource,
		NewAlertSubscriptionResource,
		NewReportSubscriptionResource,
		NewStatusPageResource,
		NewContactResource,
		NewContactGroupResource,
		NewMaintenanceResource,
	}
}

func (p *hostTrackerProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewMonitorDataSource,
		NewMonitorsDataSource,
		NewMonitorTypesDataSource,
		NewLocationsDataSource,
		NewAccountDataSource,
		NewWebhookDataSource,
		NewWebhooksDataSource,
		NewStatusPageDataSource,
		NewStatusPagesDataSource,
		NewContactDataSource,
		NewContactsDataSource,
		NewContactGroupDataSource,
		NewContactTypesDataSource,
		NewMaintenanceWindowsDataSource,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
