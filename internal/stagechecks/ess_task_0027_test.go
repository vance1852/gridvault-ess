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

type canceledBody27 struct{}

func (canceledBody27) Read([]byte) (int, error) { return 0, context.Canceled }
func (canceledBody27) Close() error             { return nil }

func TestCanceledRequestBodyKeepsUnavailableClassification0027(t *testing.T) {
	server := httpapi.NewServer(nil, nil, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", canceledBody27{})
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "request_cancelled") {
		t.Fatalf("body cancellation misclassified: %d %s", recorder.Code, recorder.Body.String())
	}
}
