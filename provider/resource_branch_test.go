//go:build !acceptance
// +build !acceptance

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

func Test_isValidBranchID(t *testing.T) {
	if os.Getenv("TF_ACC") == "1" {
		t.Skip("acceptance tests are running")
	}

	type args struct {
		s string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "happy path",
			args: args{
				s: "br-foo123b",
			},
			want: true,
		},
		{
			name: "unhappy path: no id post-prefix",
			args: args{
				s: "br-",
			},
			want: false,
		},
		{
			name: "unhappy path: no prefix",
			args: args{
				s: "qux",
			},
			want: false,
		},
		{
			name: "unhappy path: empty string",
			args: args{
				s: "",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidBranchID(tt.args.s); got != tt.want {
				t.Errorf("isValidBranchID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBranchResourceReadDesignations(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		body        string
		wantPrimary types.Bool
		wantDefault types.Bool
	}{
		{
			name: "independent designation flags",
			body: strings.Replace(branchesOneResponse,
				`"default": true`, `"default": false`, 1),
			wantPrimary: types.BoolValue(true),
			wantDefault: types.BoolValue(false),
		},
		{
			name: "explicit false primary",
			body: strings.Replace(branchesOneResponse,
				`"primary": true`, `"primary": false`, 1),
			wantPrimary: types.BoolValue(false),
			wantDefault: types.BoolValue(true),
		},
		{
			name: "absent primary on default branch",
			body: strings.Replace(branchesOneResponse,
				"      \"primary\": true,\n", "", 1),
			wantPrimary: types.BoolNull(),
			wantDefault: types.BoolValue(true),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			r := NewNeonBranchResource().(*neonBranchResource)
			var schemaResponse resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
			require.False(t, schemaResponse.Diagnostics.HasError())
			for _, name := range []string{"primary", "default"} {
				attribute, ok := schemaResponse.Schema.Attributes[name].(resourceschema.BoolAttribute)
				require.True(t, ok, "resource must expose bool %s", name)
				require.True(t, attribute.Computed)
				require.False(t, attribute.Optional)
				require.False(t, attribute.Required)
				if name == "primary" {
					require.Equal(t, "Use default instead.", attribute.DeprecationMessage)
				}
			}

			var response struct {
				Branches []json.RawMessage `json:"branches"`
			}
			require.NoError(t, json.Unmarshal([]byte(tt.body), &response))
			require.Len(t, response.Branches, 1)
			client := newBranchDataSourceTestClient(t,
				"/api/v2/projects/response-project/branches/br-first-123",
				http.StatusOK,
				fmt.Sprintf("{\n  \"branch\": %s\n}", response.Branches[0]),
			)
			var configureResponse resource.ConfigureResponse
			r.Configure(ctx, resource.ConfigureRequest{
				ProviderData: &providerAdapter{sdk: client},
			}, &configureResponse)
			require.False(t, configureResponse.Diagnostics.HasError())

			state := tfsdk.State{Schema: schemaResponse.Schema}
			diagnostics := state.Set(ctx, &neonBranchResourceModel{
				ID:        types.StringValue("br-first-123"),
				ProjectID: types.StringValue("response-project"),
			})
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			readResponse := resource.ReadResponse{State: state}
			r.Read(ctx, resource.ReadRequest{State: state}, &readResponse)
			require.False(t, readResponse.Diagnostics.HasError(), "%v", readResponse.Diagnostics)

			var primary, defaultFlag types.Bool
			diagnostics = readResponse.State.GetAttribute(ctx, path.Root("primary"), &primary)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			diagnostics = readResponse.State.GetAttribute(ctx, path.Root("default"), &defaultFlag)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.Equal(t, tt.wantPrimary, primary)
			require.Equal(t, tt.wantDefault, defaultFlag)
		})
	}
}
