package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
)

type errorBody struct {
	Error apiError `json:"error"`
}
type apiError struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	RequestID string            `json:"request_id"`
	Fields    map[string]string `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, r.Context().Err()) && r.Context().Err() != nil {
		err = fault.Wrap(fault.Unavailable, "request_cancelled", "request was cancelled", "httpapi", err)
	}
	status := fault.HTTPStatus(err)
	writeJSON(w, status, errorBody{Error: apiError{Code: fault.Code(err), Message: fault.PublicMessage(err), RequestID: RequestID(r.Context()), Fields: fault.Fields(err)}})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return fault.New(fault.Invalid, "body_required", "JSON request body is required")
		}
		return fault.Wrap(fault.Invalid, "invalid_json", "request body must be valid JSON", "httpapi.decodeJSON", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fault.New(fault.Invalid, "multiple_json_values", "request body must contain one JSON value")
	}
	return nil
}
func parseTime(value string) (resultTime time.Time, err error) {
	resultTime, err = time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fault.Wrap(fault.Invalid, "invalid_time", "timestamp must use RFC3339", "httpapi.parseTime", err)
	}
	return resultTime.UTC(), nil
}
