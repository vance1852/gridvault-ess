package stagechecks_test

import (
	"testing"

	"github.com/vance1852/gridvault-ess/internal/fault"
)

func TestFaultFieldsAreImmutableSnapshots0016(t *testing.T) {
	original := fault.WithFields(fault.New(fault.Conflict, "grid_capacity_exceeded", "capacity exceeded"), map[string]string{"available_kw": "80", "requested_kw": "100"})
	view := fault.Fields(original)
	view["available_kw"] = "redacted"
	delete(view, "requested_kw")
	fields := fault.Fields(original)
	if fields["available_kw"] != "80" || fields["requested_kw"] != "100" {
		t.Fatalf("fault fields mutated through view: %#v", fields)
	}
}
