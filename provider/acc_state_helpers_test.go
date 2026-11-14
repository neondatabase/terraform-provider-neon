package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func stateAttr(state *terraform.State, address, attr string) (string, error) {
	r, ok := state.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s not found in state", address)
	}
	v := r.Primary.Attributes[attr]
	if v == "" {
		return "", fmt.Errorf("%s.%s is empty in state", address, attr)
	}
	return v, nil
}

func captureStateAttr(address, attr string, value *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		v, err := stateAttr(state, address, attr)
		if err != nil {
			return err
		}
		*value = v
		return nil
	}
}

func TestStateAttr(t *testing.T) {
	state := terraform.NewState()
	state.RootModule().Resources["neon_project.this"] = &terraform.ResourceState{
		Primary: &terraform.InstanceState{Attributes: map[string]string{"id": "project-id"}},
	}
	for _, tc := range []struct {
		name      string
		address   string
		attr      string
		want      string
		wantError string
	}{
		{
			name:    "value",
			address: "neon_project.this",
			attr:    "id",
			want:    "project-id",
		},
		{
			name:      "missing resource",
			address:   "neon_project.missing",
			attr:      "id",
			wantError: "neon_project.missing not found in state",
		},
		{
			name:      "empty attribute",
			address:   "neon_project.this",
			attr:      "name",
			wantError: "neon_project.this.name is empty in state",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stateAttr(state, tc.address, tc.attr)
			if tc.wantError != "" {
				if err == nil || err.Error() != tc.wantError {
					t.Fatalf("stateAttr error = %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("stateAttr = %q, %v; want %q, nil", got, err, tc.want)
			}
		})
	}
}

func TestCaptureStateAttr(t *testing.T) {
	state := terraform.NewState()
	state.RootModule().Resources["neon_project.this"] = &terraform.ResourceState{
		Primary: &terraform.InstanceState{Attributes: map[string]string{"id": "project-id"}},
	}
	value := "previous"
	if err := captureStateAttr("neon_project.this", "name", &value)(state); err == nil {
		t.Fatal("expected an error for an empty attribute")
	}
	if value != "previous" {
		t.Fatalf("failed capture changed value to %q", value)
	}
	if err := captureStateAttr("neon_project.this", "id", &value)(state); err != nil {
		t.Fatal(err)
	}
	if value != "project-id" {
		t.Fatalf("captured %q, want project-id", value)
	}
}
