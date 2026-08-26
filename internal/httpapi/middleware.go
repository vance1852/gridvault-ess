package httpapi

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/service"
)

type Middleware struct {
	auth   *service.AuthService
	logger *slog.Logger
}

func NewMiddleware(auth *service.AuthService, logger *slog.Logger) *Middleware {
	return &Middleware{auth: auth, logger: logger}
}
func (m *Middleware) Request(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := requestIDFrom(r)
		ctx := withRequestID(r.Context(), requestID)
		w.Header().Set("X-Request-ID", requestID)
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if recovered := recover(); recovered != nil {
				m.logger.Error("panic recovered", "request_id", requestID, "panic", recovered, "stack", string(debug.Stack()))
				writeError(recorder, r.WithContext(ctx), fault.New(fault.Internal, "internal_error", "internal service error"))
			}
			m.logger.Info("request completed", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
		}()
		next.ServeHTTP(recorder, r.WithContext(ctx))
	})
}
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := m.auth.Authenticate(r.Context(), bearerToken(r.Header.Get("Authorization")))
		if err != nil {
			writeError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), principal)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
