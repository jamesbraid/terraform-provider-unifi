package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_dns_record"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

var _ datasource.DataSource = &dnsRecordDataSource{}

func NewDNSRecordDataSource() datasource.DataSource {
	return &dnsRecordDataSource{}
}

type dnsRecordDataSource struct {
	dataSourceWithClient
}

type dnsRecordDataSourceModel struct {
	ID      types.String         `tfsdk:"id"`
	Site    types.String         `tfsdk:"site"`
	Name    types.String         `tfsdk:"name"`
	Type    types.String         `tfsdk:"type"`
	Value   types.String         `tfsdk:"value"`
	TTL     timetypes.GoDuration `tfsdk:"ttl"`
	Enabled types.Bool           `tfsdk:"enabled"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (d *dnsRecordDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (d *dnsRecordDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_dns_record.DnsRecordDsDataSourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *dnsRecordDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data dnsRecordDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := data.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := data.Site.ValueString()
	if site == "" {
		site = d.client.Site
	}

	name := data.Name.ValueString()

	dnsRecords, err := d.client.ListDNSRecord(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading DNS Records",
			"Could not read DNS records: "+err.Error(),
		)
		return
	}

	var dnsRecord *unifi.DNSRecord
	for _, record := range dnsRecords {
		if (name == "" && record.HiddenID == "default") || record.Key == name {
			dnsRecord = &record
			break
		}
	}

	if dnsRecord == nil {
		resp.Diagnostics.AddError(
			"DNS Record Not Found",
			fmt.Sprintf("DNS record with name %s not found", name),
		)
		return
	}

	data.ID = types.StringValue(dnsRecord.ID)
	data.Site = types.StringValue(site)
	data.Name = types.StringValue(dnsRecord.Key)
	data.Type = types.StringValue(dnsRecord.RecordType)
	data.Value = types.StringValue(dnsRecord.Value)
	if dnsRecord.Ttl != 0 {
		data.TTL = util.DurationValue(dnsRecord.Ttl, time.Second)
	} else {
		data.TTL = timetypes.NewGoDurationNull()
	}
	data.Enabled = types.BoolValue(dnsRecord.Enabled)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
