package alarm

import (
	"errors"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"testing"
	"time"
)

var alarmNow = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func validAlarm(t *testing.T) Alarm {
	t.Helper()
	value, err := Create(NewAlarm{SiteID: "site", ClusterID: "cluster", Type: "soc_low", Severity: Critical, Fingerprint: "cluster:soc_low", Message: "cluster SOC is critically low"}, alarmNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestAlarmLifecycleRequiresAcknowledgementAndRecovery(t *testing.T) {
	value := validAlarm(t)
	if value.Status != Open || value.Version != 1 {
		t.Fatalf("new=%+v", value)
	}
	if _, err := value.Resolve("operator", value.Version, true, alarmNow); fault.Code(err) != "invalid_alarm_transition" {
		t.Fatalf("early resolution=%v", err)
	}
	acknowledged, err := value.Acknowledge("operator", value.Version, alarmNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if acknowledged.Status != Acknowledged || acknowledged.AcknowledgedBy != "operator" || acknowledged.AcknowledgedAt == nil {
		t.Fatalf("ack=%+v", acknowledged)
	}
	if _, err = acknowledged.Resolve("operator", acknowledged.Version, false, alarmNow); fault.Code(err) != "condition_not_recovered" {
		t.Fatalf("unhealthy=%v", err)
	}
	resolved, err := acknowledged.Resolve("operator", acknowledged.Version, true, alarmNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != Resolved || resolved.ResolvedBy != "operator" || resolved.ResolvedAt == nil {
		t.Fatalf("resolved=%+v", resolved)
	}
}
func TestAlarmVersionAndActorRules(t *testing.T) {
	value := validAlarm(t)
	if _, err := value.Acknowledge("operator", 0, alarmNow); !errors.Is(err, fault.ErrVersionConflict) {
		t.Fatalf("version=%v", err)
	}
	if _, err := value.Acknowledge("", value.Version, alarmNow); fault.Code(err) != "actor_required" {
		t.Fatalf("actor=%v", err)
	}
	ack, _ := value.Acknowledge("operator", value.Version, alarmNow)
	if _, err := ack.Resolve("", ack.Version, true, alarmNow); fault.Code(err) != "actor_required" {
		t.Fatalf("resolver=%v", err)
	}
}
func TestAlarmEscalation(t *testing.T) {
	value := validAlarm(t)
	value.Severity = Warning
	escalated, err := value.Escalate("temperature continues rising", alarmNow)
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Severity != Critical || escalated.Message != "temperature continues rising" {
		t.Fatalf("escalated=%+v", escalated)
	}
	resolved := value
	resolved.Status = Resolved
	if _, err = resolved.Escalate("again", alarmNow); fault.Code(err) != "alarm_resolved" {
		t.Fatalf("resolved escalation=%v", err)
	}
	if _, err = value.Escalate("", alarmNow); fault.Code(err) != "message_required" {
		t.Fatalf("message=%v", err)
	}
}
func TestAlarmCreationValidation(t *testing.T) {
	base := NewAlarm{SiteID: "site", ClusterID: "cluster", Type: "type", Severity: Warning, Fingerprint: "fingerprint", Message: "valid alarm message"}
	tests := []struct {
		name   string
		mutate func(*NewAlarm)
		code   string
	}{{"site", func(v *NewAlarm) { v.SiteID = "" }, "missing_alarm_owner"}, {"cluster", func(v *NewAlarm) { v.ClusterID = "" }, "missing_alarm_owner"}, {"type", func(v *NewAlarm) { v.Type = "" }, "invalid_alarm_identity"}, {"fingerprint", func(v *NewAlarm) { v.Fingerprint = "" }, "invalid_alarm_identity"}, {"severity", func(v *NewAlarm) { v.Severity = "info" }, "invalid_alarm_severity"}, {"message short", func(v *NewAlarm) { v.Message = "bad" }, "invalid_alarm_message"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := base
			tt.mutate(&input)
			_, err := Create(input, alarmNow)
			if fault.Code(err) != tt.code {
				t.Fatalf("code=%s err=%v", fault.Code(err), err)
			}
		})
	}
}
func TestBatchResultCountsIndependentOutcomes(t *testing.T) {
	result := BuildBatchResult([]ItemResult{{ID: "a", Status: "acknowledged"}, {ID: "b", Status: "failed", ErrorCode: "version_conflict"}, {ID: "c", Status: "acknowledged"}})
	if result.Succeeded != 2 || result.Failed != 1 || len(result.Items) != 3 {
		t.Fatalf("result=%+v", result)
	}
	result.Items[0].Status = "changed"
	original := BuildBatchResult([]ItemResult{{ID: "a", Status: "ok"}})
	if original.Items[0].Status != "ok" {
		t.Fatal("batch storage was shared")
	}
}
