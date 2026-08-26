package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/vance1852/gridvault-ess/internal/service"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	principalKey contextKey = "principal"
)

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}
func Principal(ctx context.Context) (service.Principal, bool) {
	value, ok := ctx.Value(principalKey).(service.Principal)
	return value, ok
}
func requestIDFrom(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if len(value) >= 8 && len(value) <= 128 {
		return value
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "request-unknown"
	}
	return hex.EncodeToString(random)
}
func withRequestID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, requestIDKey, value)
}
func withPrincipal(ctx context.Context, value service.Principal) context.Context {
	return context.WithValue(ctx, principalKey, value)
}
func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
