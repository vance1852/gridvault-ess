package stagechecks_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vance1852/gridvault-ess/internal/httpapi"
)

type canceledReadiness20 struct{}

func (canceledReadiness20) Ping(ctx context.Context) error { return ctx.Err() }

func TestReadinessCancellationPreservesHTTPClassification0020(t *testing.T) {
	server := httpapi.NewServer(nil, nil, nil, nil, nil, canceledReadiness20{}, slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "request_cancelled") {
		t.Fatalf("cancellation misclassified: %d %s", recorder.Code, recorder.Body.String())
	}
}
