package idempotency

import (
	"github.com/vance1852/gridvault-ess/internal/fault"
	"testing"
	"time"
)

var idemNow = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func TestIdempotencyLifecycle(t *testing.T) {
	request := []byte(`{"name":"plan"}`)
	record, err := Start("actor", "post", "/v1/plans", "request-123", request, idemNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if record.Method != "POST" || record.State != Started || record.RequestHash == "" {
		t.Fatalf("record=%+v", record)
	}
	if !record.Matches(request) || record.Matches([]byte("different")) {
		t.Fatal("request hash matching failed")
	}
	completed, err := record.Complete(201, []byte(`{"id":"plan-1"}`), idemNow.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	status, body, err := completed.Replay(request, idemNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status != 201 || string(body) != `{"id":"plan-1"}` {
		t.Fatalf("replay=%d %s", status, body)
	}
	body[0] = 'x'
	if string(completed.ResponseBody) != `{"id":"plan-1"}` {
		t.Fatal("replay leaked response slice")
	}
}
func TestIdempotencyRejectsPayloadReuse(t *testing.T) {
	record, _ := Start("actor", "POST", "/path", "request-123", []byte("one"), idemNow, time.Hour)
	completed, _ := record.Complete(200, []byte("ok"), idemNow)
	if _, _, err := completed.Replay([]byte("two"), idemNow); fault.Code(err) != "idempotency_payload_mismatch" {
		t.Fatalf("mismatch=%v", err)
	}
	if _, _, err := record.Replay([]byte("one"), idemNow); fault.Code(err) != "idempotency_in_progress" {
		t.Fatalf("started=%v", err)
	}
	if _, _, err := completed.Replay([]byte("one"), idemNow.Add(time.Hour)); fault.Code(err) != "idempotency_expired" {
		t.Fatalf("expired=%v", err)
	}
}
func TestIdempotencyStartValidation(t *testing.T) {
	base := struct {
		actor, method, path, key string
		ttl                      time.Duration
	}{"actor", "POST", "/path", "request-123", time.Hour}
	tests := []struct {
		name   string
		mutate func(*struct {
			actor, method, path, key string
			ttl                      time.Duration
		})
		code string
	}{{"actor", func(v *struct {
		actor, method, path, key string
		ttl                      time.Duration
	}) {
		v.actor = ""
	}, "invalid_idempotency_scope"}, {"method", func(v *struct {
		actor, method, path, key string
		ttl                      time.Duration
	}) {
		v.method = ""
	}, "invalid_idempotency_scope"}, {"path", func(v *struct {
		actor, method, path, key string
		ttl                      time.Duration
	}) {
		v.path = ""
	}, "invalid_idempotency_scope"}, {"key missing", func(v *struct {
		actor, method, path, key string
		ttl                      time.Duration
	}) {
		v.key = ""
	}, "invalid_idempotency_scope"}, {"key short", func(v *struct {
		actor, method, path, key string
		ttl                      time.Duration
	}) {
		v.key = "short"
	}, "invalid_idempotency_key"}, {"ttl short", func(v *struct {
		actor, method, path, key string
		ttl                      time.Duration
	}) {
		v.ttl = time.Second
	}, "invalid_idempotency_ttl"}, {"ttl long", func(v *struct {
		actor, method, path, key string
		ttl                      time.Duration
	}) {
		v.ttl = 8 * 24 * time.Hour
	}, "invalid_idempotency_ttl"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := base
			tt.mutate(&input)
			_, err := Start(input.actor, input.method, input.path, input.key, nil, idemNow, input.ttl)
			if fault.Code(err) != tt.code {
				t.Fatalf("code=%s err=%v", fault.Code(err), err)
			}
		})
	}
}
func TestIdempotencyCompleteValidation(t *testing.T) {
	record, _ := Start("actor", "POST", "/path", "request-123", nil, idemNow, time.Hour)
	if _, err := record.Complete(99, nil, idemNow); fault.Code(err) != "invalid_response_status" {
		t.Fatalf("low status=%v", err)
	}
	if _, err := record.Complete(600, nil, idemNow); fault.Code(err) != "invalid_response_status" {
		t.Fatalf("high status=%v", err)
	}
	large := make([]byte, (1<<20)+1)
	if _, err := record.Complete(200, large, idemNow); fault.Code(err) != "response_too_large" {
		t.Fatalf("large=%v", err)
	}
	completed, _ := record.Complete(200, nil, idemNow)
	if _, err := completed.Complete(200, nil, idemNow); fault.Code(err) != "idempotency_completed" {
		t.Fatalf("again=%v", err)
	}
}
