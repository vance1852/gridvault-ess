package fault

import (
	"errors"
	"fmt"
)

type Kind string

const (
	Invalid         Kind = "invalid"
	Unauthenticated Kind = "unauthenticated"
	Forbidden       Kind = "forbidden"
	NotFound        Kind = "not_found"
	Conflict        Kind = "conflict"
	Unavailable     Kind = "unavailable"
	Internal        Kind = "internal"
)

// Error preserves a stable public classification while retaining its cause.
type Error struct {
	Kind      Kind
	Code      string
	Message   string
	Operation string
	Cause     error
	Fields    map[string]string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Operation != "" {
		return fmt.Sprintf("%s: %s: %s", e.Operation, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func Wrap(kind Kind, code, message, operation string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Message: message, Operation: operation, Cause: cause}
}

func WithFields(err *Error, fields map[string]string) *Error {
	copyFields := make(map[string]string, len(fields))
	for key, value := range fields {
		copyFields[key] = value
	}
	clone := *err
	clone.Fields = copyFields
	return &clone
}

func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func IsKind(err error, kind Kind) bool {
	target, ok := As(err)
	return ok && target.Kind == kind
}

func CommitsFailedTransaction(err error) bool {
	target, ok := As(err)
	if !ok {
		return false
	}
	return target.Kind == Conflict && target.Code == "reservation_conflict"
}

func Code(err error) string {
	if target, ok := As(err); ok {
		return target.Code
	}
	return "internal_error"
}

func PublicMessage(err error) string {
	if target, ok := As(err); ok && target.Message != "" {
		return target.Message
	}
	return "internal service error"
}

func Fields(err error) map[string]string {
	target, ok := As(err)
	if !ok || len(target.Fields) == 0 {
		return nil
	}
	result := make(map[string]string, len(target.Fields))
	for key, value := range target.Fields {
		result[key] = value
	}
	return result
}

func HTTPStatus(err error) int {
	target, ok := As(err)
	if !ok {
		return 500
	}
	switch target.Kind {
	case Invalid:
		return 400
	case Unauthenticated:
		return 401
	case Forbidden:
		return 403
	case NotFound:
		return 404
	case Conflict:
		return 409
	case Unavailable:
		return 503
	default:
		return 500
	}
}

var (
	ErrNotFound        = New(NotFound, "not_found", "requested resource was not found")
	ErrVersionConflict = New(Conflict, "version_conflict", "resource changed concurrently")
	ErrUnauthorized    = New(Unauthenticated, "authentication_required", "valid authentication is required")
	ErrPermission      = New(Forbidden, "permission_denied", "role is not allowed to perform this action")
)
