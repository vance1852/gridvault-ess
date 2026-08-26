package audit

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Event struct {
	ID, ActorID, RequestID, ObjectType, ObjectID, Action, Result, DetailsJSON string
	OccurredAt                                                                time.Time
}

func NewEvent(actorID, requestID, objectType, objectID, action, result string, details any, now time.Time) (Event, error) {
	requestID = strings.TrimSpace(requestID)
	objectType = strings.TrimSpace(objectType)
	objectID = strings.TrimSpace(objectID)
	action = strings.TrimSpace(action)
	result = strings.TrimSpace(result)
	if requestID == "" || objectType == "" || objectID == "" || action == "" || result == "" {
		return Event{}, fault.New(fault.Invalid, "invalid_audit_event", "audit request, object, action, and result are required")
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return Event{}, fault.Wrap(fault.Invalid, "invalid_audit_details", "audit details cannot be encoded", "audit.NewEvent", err)
	}
	if len(payload) > 32*1024 {
		return Event{}, fault.New(fault.Invalid, "audit_details_too_large", "audit details exceed 32 KiB")
	}
	return Event{ID: uuid.NewString(), ActorID: strings.TrimSpace(actorID), RequestID: requestID, ObjectType: objectType, ObjectID: objectID, Action: action, Result: result, DetailsJSON: string(payload), OccurredAt: now.UTC()}, nil
}

type Cursor struct {
	OccurredAt time.Time
	ID         string
}
type Filter struct {
	ObjectType, ObjectID, RequestID string
	From, Until                     *time.Time
	Cursor                          *Cursor
	Limit                           int
}

func (f Filter) Normalize() (Filter, error) {
	copy := f
	copy.ObjectType = strings.TrimSpace(copy.ObjectType)
	copy.ObjectID = strings.TrimSpace(copy.ObjectID)
	copy.RequestID = strings.TrimSpace(copy.RequestID)
	if copy.Limit == 0 {
		copy.Limit = 100
	}
	if copy.Limit < 1 || copy.Limit > 500 {
		return Filter{}, fault.New(fault.Invalid, "invalid_page_limit", "audit page limit must be between 1 and 500")
	}
	if copy.From != nil {
		value := copy.From.UTC()
		copy.From = &value
	}
	if copy.Until != nil {
		value := copy.Until.UTC()
		copy.Until = &value
	}
	if copy.From != nil && copy.Until != nil && !copy.From.Before(*copy.Until) {
		return Filter{}, fault.New(fault.Invalid, "invalid_filter_window", "audit filter end must follow start")
	}
	if copy.Cursor != nil && (copy.Cursor.OccurredAt.IsZero() || strings.TrimSpace(copy.Cursor.ID) == "") {
		return Filter{}, fault.New(fault.Invalid, "invalid_cursor", "audit cursor is invalid")
	}
	return copy, nil
}

type Page struct {
	Items []Event
	Next  *Cursor
}
