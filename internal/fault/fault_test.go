package fault

import "testing"

func TestFieldsReturnsCopyNotAlias(t *testing.T) {
	err := WithFields(
		New(Conflict, "invalid_plan_transition", "dispatch plan transition is not allowed"),
		map[string]string{"current": "draft", "next": "approved"},
	)
	first := Fields(err)
	if first["current"] != "draft" || first["next"] != "approved" {
		t.Fatalf("first read returned wrong values: %v", first)
	}
	// Simulate a layer that reads structured fields, scrubs them for logging,
	// then mutates the returned map.
	delete(first, "next")
	first["current"] = "redacted"

	second := Fields(err)
	if second["current"] != "draft" || second["next"] != "approved" {
		t.Fatalf("error fields leaked mutations from prior reader: %v", second)
	}
}

func TestFieldsNilWhenEmpty(t *testing.T) {
	if got := Fields(New(Invalid, "missing", "required")); got != nil {
		t.Fatalf("expected nil fields, got %v", got)
	}
	if got := Fields(nil); got != nil {
		t.Fatalf("expected nil for nil error, got %v", got)
	}
}

func TestWithFieldsDoesNotAliasInput(t *testing.T) {
	input := map[string]string{"status": "active"}
	err := WithFields(New(Conflict, "site_not_active", "site cannot accept dispatch plans"), input)
	input["status"] = "mutated"
	if got := Fields(err)["status"]; got != "active" {
		t.Fatalf("WithFields aliased caller map: got %q", got)
	}
}
