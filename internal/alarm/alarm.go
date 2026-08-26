package alarm

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Status string
type Severity string

const (
	Open         Status   = "open"
	Acknowledged Status   = "acknowledged"
	Resolved     Status   = "resolved"
	Warning      Severity = "warning"
	Critical     Severity = "critical"
)

type Alarm struct {
	ID             string
	SiteID         string
	ClusterID      string
	Type           string
	Severity       Severity
	Status         Status
	Fingerprint    string
	Message        string
	OpenedAt       time.Time
	AcknowledgedBy string
	AcknowledgedAt *time.Time
	ResolvedBy     string
	ResolvedAt     *time.Time
	Version        int64
	UpdatedAt      time.Time
}

type NewAlarm struct {
	SiteID, ClusterID, Type, Fingerprint, Message string
	Severity                                      Severity
}

func Create(input NewAlarm, now time.Time) (Alarm, error) {
	if strings.TrimSpace(input.SiteID) == "" || strings.TrimSpace(input.ClusterID) == "" {
		return Alarm{}, fault.New(fault.Invalid, "missing_alarm_owner", "site and cluster are required")
	}
	if strings.TrimSpace(input.Type) == "" || strings.TrimSpace(input.Fingerprint) == "" {
		return Alarm{}, fault.New(fault.Invalid, "invalid_alarm_identity", "alarm type and fingerprint are required")
	}
	if input.Severity != Warning && input.Severity != Critical {
		return Alarm{}, fault.New(fault.Invalid, "invalid_alarm_severity", "alarm severity must be warning or critical")
	}
	message := strings.TrimSpace(input.Message)
	if len(message) < 5 || len(message) > 500 {
		return Alarm{}, fault.New(fault.Invalid, "invalid_alarm_message", "alarm message must contain 5 to 500 characters")
	}
	now = now.UTC()
	return Alarm{ID: uuid.NewString(), SiteID: input.SiteID, ClusterID: input.ClusterID, Type: input.Type, Severity: input.Severity, Status: Open, Fingerprint: input.Fingerprint, Message: message, OpenedAt: now, Version: 1, UpdatedAt: now}, nil
}

func (a Alarm) Acknowledge(actorID string, expectedVersion int64, now time.Time) (Alarm, error) {
	if a.Version != expectedVersion {
		return Alarm{}, fault.ErrVersionConflict
	}
	if a.Status != Open {
		return Alarm{}, transitionError(a.Status, Acknowledged)
	}
	if strings.TrimSpace(actorID) == "" {
		return Alarm{}, fault.New(fault.Invalid, "actor_required", "alarm acknowledgement actor is required")
	}
	at := now.UTC()
	copy := a
	copy.Status = Acknowledged
	copy.AcknowledgedBy = actorID
	copy.AcknowledgedAt = &at
	copy.Version++
	copy.UpdatedAt = at
	return copy, nil
}

func (a Alarm) Resolve(actorID string, expectedVersion int64, recovered bool, now time.Time) (Alarm, error) {
	if a.Version != expectedVersion {
		return Alarm{}, fault.ErrVersionConflict
	}
	if a.Status != Acknowledged {
		return Alarm{}, transitionError(a.Status, Resolved)
	}
	if !recovered {
		return Alarm{}, fault.New(fault.Conflict, "condition_not_recovered", "alarm condition must recover before resolution")
	}
	if strings.TrimSpace(actorID) == "" {
		return Alarm{}, fault.New(fault.Invalid, "actor_required", "alarm resolution actor is required")
	}
	at := now.UTC()
	copy := a
	copy.Status = Resolved
	copy.ResolvedBy = actorID
	copy.ResolvedAt = &at
	copy.Version++
	copy.UpdatedAt = at
	return copy, nil
}

func (a Alarm) Escalate(message string, now time.Time) (Alarm, error) {
	if a.Status == Resolved {
		return Alarm{}, fault.New(fault.Conflict, "alarm_resolved", "resolved alarm cannot be escalated")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return Alarm{}, fault.New(fault.Invalid, "message_required", "escalation message is required")
	}
	copy := a
	copy.Severity = Critical
	copy.Message = message
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func transitionError(current, next Status) error {
	return fault.WithFields(fault.New(fault.Conflict, "invalid_alarm_transition", "alarm transition is not allowed"), map[string]string{"current": string(current), "next": string(next)})
}

type Filter struct {
	SiteID        string
	Status        Status
	Severity      Severity
	Limit, Offset int
}

func (f Filter) Normalize() (Filter, error) {
	copy := f
	copy.SiteID = strings.TrimSpace(copy.SiteID)
	if copy.Limit == 0 {
		copy.Limit = 50
	}
	if copy.Limit < 1 || copy.Limit > 200 {
		return Filter{}, fault.New(fault.Invalid, "invalid_page_limit", "page limit must be between 1 and 200")
	}
	if copy.Offset < 0 {
		return Filter{}, fault.New(fault.Invalid, "invalid_page_offset", "page offset cannot be negative")
	}
	return copy, nil
}

type ItemResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
}
type BatchResult struct {
	Items     []ItemResult `json:"items"`
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
}

func retainBatchItems(results []ItemResult) []ItemResult {
	if len(results) == 0 {
		return nil
	}
	return results[:len(results)]
}

func BuildBatchResult(results []ItemResult) BatchResult {
	output := BatchResult{Items: retainBatchItems(results)}
	for _, item := range results {
		if item.ErrorCode == "" {
			output.Succeeded++
		} else {
			output.Failed++
		}
	}
	return output
}
