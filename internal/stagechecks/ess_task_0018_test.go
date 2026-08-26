package stagechecks_test

import (
	"testing"

	"github.com/vance1852/gridvault-ess/internal/alarm"
)

func TestAlarmBatchOwnsResultItems0018(t *testing.T) {
	items := []alarm.ItemResult{{ID: "alarm-a", Status: "acknowledged"}, {ID: "alarm-b", Status: "failed", ErrorCode: "version_conflict"}}
	batch := alarm.BuildBatchResult(items)
	items[0] = alarm.ItemResult{ID: "replacement", Status: "failed", ErrorCode: "late"}
	if batch.Items[0].ID != "alarm-a" || batch.Items[0].Status != "acknowledged" || batch.Succeeded != 1 || batch.Failed != 1 {
		t.Fatalf("batch result changed after input reuse: %#v", batch)
	}
}
