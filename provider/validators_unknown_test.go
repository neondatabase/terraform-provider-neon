package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

// Values that come from other resources are unknown at plan time.
// Validators must skip them; Terraform validates again once they are known.
// Null and invalid known values must still fail.

func TestStringValidatorsSkipUnknown(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		validator validator.String
		value     types.String
		wantError bool
	}{
		{"endpoint type unknown", validateEndpointType, types.StringUnknown(), false},
		{"endpoint type null", validateEndpointType, types.StringNull(), false},
		{"endpoint type valid", validateEndpointType, types.StringValue("read_only"), false},
		{"endpoint type invalid", validateEndpointType, types.StringValue("bogus"), true},
		{"compute provisioner unknown", validateComputeProvisioner, types.StringUnknown(), false},
		{"compute provisioner valid", validateComputeProvisioner, types.StringValue("k8s-neonvm"), false},
		{"compute provisioner invalid", validateComputeProvisioner, types.StringValue("bogus"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var resp validator.StringResponse
			tt.validator.ValidateString(context.Background(),
				validator.StringRequest{ConfigValue: tt.value}, &resp)
			require.Equal(t, tt.wantError, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		})
	}
}

func TestMaintenanceWindowValidatorSkipsUnknown(t *testing.T) {
	t.Parallel()

	valid := map[string]attr.Value{
		"weekdays":   types.ListValueMust(types.Int32Type, []attr.Value{types.Int32Value(6)}),
		"start_time": types.StringValue("01:00"),
		"end_time":   types.StringValue("02:00"),
	}
	with := func(name string, v attr.Value) map[string]attr.Value {
		out := map[string]attr.Value{}
		for k, val := range valid {
			out[k] = val
		}
		out[name] = v
		return out
	}

	for _, tt := range []struct {
		name      string
		attrs     map[string]attr.Value
		wantError string
	}{
		{name: "all known and valid", attrs: valid},
		{name: "start_time unknown", attrs: with("start_time", types.StringUnknown())},
		{name: "end_time unknown", attrs: with("end_time", types.StringUnknown())},
		{name: "weekdays unknown", attrs: with("weekdays", types.ListUnknown(types.Int32Type))},
		{name: "weekdays with an unknown item", attrs: with("weekdays",
			types.ListValueMust(types.Int32Type, []attr.Value{types.Int32Value(1), types.Int32Unknown()}))},
		{name: "start_time null", attrs: with("start_time", types.StringNull()), wantError: "start_time is required"},
		{name: "end_time null", attrs: with("end_time", types.StringNull()), wantError: "end_time is required"},
		{name: "weekdays null", attrs: with("weekdays", types.ListNull(types.Int32Type)), wantError: "undefined weekdays"},
		{name: "start_time invalid", attrs: with("start_time", types.StringValue("25:99")), wantError: "invalid start_time"},
		{name: "weekday out of range", attrs: with("weekdays",
			types.ListValueMust(types.Int32Type, []attr.Value{types.Int32Value(8)})), wantError: "invalid weekday"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			window := types.ObjectValueMust(map[string]attr.Type{
				"weekdays":   types.ListType{ElemType: types.Int32Type},
				"start_time": types.StringType,
				"end_time":   types.StringType,
			}, tt.attrs)

			var resp validator.ObjectResponse
			(&validatorMaintenanceWindow{}).ValidateObject(context.Background(),
				validator.ObjectRequest{ConfigValue: window}, &resp)

			if tt.wantError == "" {
				require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
				return
			}
			require.True(t, resp.Diagnostics.HasError(), "expected an error")
			var details []string
			for _, d := range resp.Diagnostics.Errors() {
				details = append(details, d.Detail())
			}
			require.Contains(t, details[0], tt.wantError, "%v", details)
		})
	}
}
