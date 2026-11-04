package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestBuildTriggerUpdateRequest_serializes(t *testing.T) {
	for _, typ := range []string{"schedule", "storage_object_created"} {
		t.Run(typ, func(t *testing.T) {
			plan := neonTriggerResourceModel{
				Type:         types.StringValue(typ),
				Name:         types.StringValue("nightly"),
				FunctionSlug: types.StringValue("worker"),
				FunctionPath: types.StringNull(),
				Enabled:      types.BoolValue(true),
				Cron:         types.StringValue("0 3 * * *"),
				BucketName:   types.StringValue("uploads"),
				Prefix:       types.StringNull(),
			}

			raw, err := json.Marshal(buildTriggerUpdateRequest(&plan))
			if err != nil {
				t.Fatal(err)
			}

			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got["type"] != typ {
				t.Errorf("type = %v, want %q", got["type"], typ)
			}
		})
	}
}
