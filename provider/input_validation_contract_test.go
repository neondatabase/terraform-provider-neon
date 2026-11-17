package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

func TestInputValidationContract(t *testing.T) {
	data, err := os.ReadFile("testdata/input-validation-contract.json")
	require.NoError(t, err)
	var contract struct {
		Inputs []struct {
			Surface        string   `json:"surface"`
			Path           string   `json:"path"`
			Enum           []string `json:"enum"`
			MinItems       int      `json:"min_items"`
			MaxItems       int      `json:"max_items"`
			RejectedValues []string `json:"rejected_values"`
		} `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(data, &contract))
	require.NotEmpty(t, contract.Inputs)
	ctx := context.Background()
	for _, input := range contract.Inputs {
		t.Run(input.Surface+"/"+input.Path, func(t *testing.T) {
			require.NotEmpty(t, input.Enum)
			require.NotEmpty(t, input.RejectedValues)
			attributePath := path.Root(input.Path)
			var schemaResp resource.SchemaResponse
			switch input.Surface {
			case "neon_data_api":
				NewNeonDataAPIResource().Schema(ctx, resource.SchemaRequest{}, &schemaResp)
			case "neon_service_credential":
				NewNeonServiceCredentialResource().Schema(ctx, resource.SchemaRequest{}, &schemaResp)
			default:
				t.Fatalf("no behavior test owner for %s", input.Surface)
			}
			require.False(t, schemaResp.Diagnostics.HasError(), "%v", schemaResp.Diagnostics)
			attribute, ok := schemaResp.Schema.Attributes[input.Path]
			require.True(t, ok, "missing contract input %s", input.Path)

			var validate func(types.String) diag.Diagnostics
			switch attribute := attribute.(type) {
			case schema.StringAttribute:
				validate = func(value types.String) diag.Diagnostics {
					var resp validator.StringResponse
					for _, v := range attribute.Validators {
						v.ValidateString(ctx, validator.StringRequest{
							Path:        attributePath,
							ConfigValue: value,
						}, &resp)
					}
					return resp.Diagnostics
				}
			case schema.ListAttribute:
				validateList := func(value types.List) diag.Diagnostics {
					var resp validator.ListResponse
					for _, v := range attribute.Validators {
						v.ValidateList(ctx, validator.ListRequest{
							Path:        attributePath,
							ConfigValue: value,
						}, &resp)
					}
					return resp.Diagnostics
				}
				require.Positive(t, input.MinItems)
				require.GreaterOrEqual(t, input.MaxItems, input.MinItems)
				repeatedList := func(value types.String, size int) types.List {
					values := make([]attr.Value, size)
					for i := range values {
						values[i] = value
					}
					return types.ListValueMust(types.StringType, values)
				}
				validate = func(value types.String) diag.Diagnostics {
					return validateList(repeatedList(value, input.MinItems))
				}
				for _, size := range []int{input.MinItems - 1, input.MinItems, input.MaxItems, input.MaxItems + 1} {
					t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
						diagnostics := validateList(repeatedList(types.StringValue(input.Enum[0]), size))
						wantError := size < input.MinItems || size > input.MaxItems
						require.Equal(t, wantError, diagnostics.HasError(), "%v", diagnostics)
					})
				}
				require.Empty(t, validateList(types.ListNull(types.StringType)))
				require.Empty(t, validateList(types.ListUnknown(types.StringType)))
			default:
				t.Fatalf("no behavior test for attribute type %T", attribute)
			}

			for _, value := range input.Enum {
				t.Run("accept_"+value, func(t *testing.T) {
					require.Empty(t, validate(types.StringValue(value)))
					require.Empty(t, validate(types.StringValue(`"`+value+`"`)))
				})
			}
			require.Empty(t, validate(types.StringNull()))
			require.Empty(t, validate(types.StringUnknown()))
			for _, value := range input.RejectedValues {
				t.Run("reject_"+value, func(t *testing.T) {
					diagnostics := validate(types.StringValue(value))
					require.True(t, diagnostics.HasError(), "expected rejection of %q", value)
					for _, diagnostic := range diagnostics {
						if value != "" {
							require.NotContains(t, diagnostic.Detail(), value)
						}
						withPath, ok := diagnostic.(diag.DiagnosticWithPath)
						require.True(t, ok, "contract diagnostic must identify its input")
						require.True(t, attributePath.Equal(withPath.Path()) || attributePath.AtListIndex(0).Equal(withPath.Path()), "%s", withPath.Path())
						for _, allowed := range input.Enum {
							require.Contains(t, diagnostic.Detail(), allowed)
						}
					}
				})
			}
		})
	}
}
