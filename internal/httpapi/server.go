package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vance1852/gridvault-ess/internal/alarm"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/service"
	"github.com/vance1852/gridvault-ess/internal/site"
	"github.com/vance1852/gridvault-ess/internal/telemetry"
)

type Readiness interface{ Ping(context.Context) error }
type Server struct {
	auth      *service.AuthService
	sites     *service.SiteService
	dispatch  *service.DispatchService
	telemetry *service.TelemetryService
	alarms    *service.AlarmService
	ready     Readiness
	logger    *slog.Logger
	maxBytes  int64
	handler   http.Handler
}

func NewServer(auth *service.AuthService, sites *service.SiteService, dispatchService *service.DispatchService, telemetryService *service.TelemetryService, alarmService *service.AlarmService, ready Readiness, logger *slog.Logger, maxBytes int64) *Server {
	s := &Server{auth: auth, sites: sites, dispatch: dispatchService, telemetry: telemetryService, alarms: alarmService, ready: ready, logger: logger, maxBytes: maxBytes}
	s.handler = s.routes()
	return s
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	middleware := NewMiddleware(s.auth, s.logger)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.readiness)
	mux.HandleFunc("POST /v1/auth/login", s.login)
	protected := http.NewServeMux()
	protected.HandleFunc("POST /v1/auth/logout", s.logout)
	protected.HandleFunc("GET /v1/sites", s.listSites)
	protected.HandleFunc("POST /v1/sites", s.createSite)
	protected.HandleFunc("POST /v1/sites/{id}/transition", s.transitionSite)
	protected.HandleFunc("POST /v1/sites/{id}/clusters", s.createCluster)
	protected.HandleFunc("GET /v1/dispatch-plans", s.listPlans)
	protected.HandleFunc("POST /v1/dispatch-plans", s.createPlan)
	protected.HandleFunc("GET /v1/dispatch-plans/{id}", s.getPlan)
	protected.HandleFunc("POST /v1/dispatch-plans/{id}/submit", s.submitPlan)
	protected.HandleFunc("POST /v1/dispatch-plans/{id}/approve", s.approvePlan)
	protected.HandleFunc("POST /v1/dispatch-plans/{id}/dispatch", s.dispatchPlan)
	protected.HandleFunc("POST /v1/telemetry", s.recordTelemetry)
	protected.HandleFunc("POST /v1/alarms/{id}/acknowledge", s.acknowledgeAlarm)
	protected.HandleFunc("POST /v1/alarms/{id}/resolve", s.resolveAlarm)
	protected.HandleFunc("POST /v1/alarms/batch-acknowledge", s.batchAcknowledge)
	mux.Handle("/v1/", middleware.Authenticate(protected))
	return middleware.Request(mux)
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "alive", "time": time.Now().UTC()})
}
func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.ready.Ping(ctx); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
func principalOrError(w http.ResponseWriter, r *http.Request) (service.Principal, bool) {
	p, ok := Principal(r.Context())
	if !ok {
		writeError(w, r, fault.ErrUnauthorized)
		return service.Principal{}, false
	}
	return p, true
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.auth.Login(r.Context(), service.LoginInput{Email: input.Email, Password: input.Password, RequestID: RequestID(r.Context())})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	if err := s.auth.Logout(r.Context(), p, RequestID(r.Context())); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) createSite(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input site.NewSite
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	entity, err := s.sites.Create(r.Context(), p, input, RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, entity)
}
func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	limit, err := queryInt(r, "limit", 50)
	if err != nil {
		writeError(w, r, err)
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil {
		writeError(w, r, err)
		return
	}
	page, err := s.sites.List(r.Context(), p, site.ListFilter{Status: site.Status(r.URL.Query().Get("status")), Search: r.URL.Query().Get("search"), Sort: site.Sort(r.URL.Query().Get("sort")), Limit: limit, Offset: offset})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (s *Server) transitionSite(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input struct {
		Status  site.Status `json:"status"`
		Version int64       `json:"version"`
	}
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	entity, err := s.sites.Transition(r.Context(), p, r.PathValue("id"), input.Status, input.Version, RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entity)
}
func (s *Server) createCluster(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input site.NewCluster
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	input.SiteID = r.PathValue("id")
	entity, err := s.sites.CreateCluster(r.Context(), p, input, RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, entity)
}
func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input struct {
		SiteID      string             `json:"site_id"`
		Name        string             `json:"name"`
		Direction   dispatch.Direction `json:"direction"`
		RequestedKW int64              `json:"requested_kw"`
		TargetKWh   int64              `json:"target_kwh"`
		StartsAt    string             `json:"starts_at"`
		EndsAt      string             `json:"ends_at"`
	}
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	start, err := parseTime(input.StartsAt)
	if err != nil {
		writeError(w, r, err)
		return
	}
	end, err := parseTime(input.EndsAt)
	if err != nil {
		writeError(w, r, err)
		return
	}
	plan, err := s.dispatch.Create(r.Context(), p, dispatch.NewPlan{SiteID: input.SiteID, Name: input.Name, Direction: input.Direction, RequestedKW: input.RequestedKW, TargetKWh: input.TargetKWh, StartsAt: start, EndsAt: end}, RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}
func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	plan, err := s.dispatch.Get(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}
func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	limit, err := queryInt(r, "limit", 50)
	if err != nil {
		writeError(w, r, err)
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil {
		writeError(w, r, err)
		return
	}
	page, err := s.dispatch.List(r.Context(), p, dispatch.PlanFilter{SiteID: r.URL.Query().Get("site_id"), Status: dispatch.PlanStatus(r.URL.Query().Get("status")), Search: r.URL.Query().Get("search"), Limit: limit, Offset: offset, Newest: r.URL.Query().Get("newest") == "true"})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (s *Server) submitPlan(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input struct {
		Version    int64    `json:"version"`
		ClusterIDs []string `json:"cluster_ids"`
	}
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	plan, err := s.dispatch.Submit(r.Context(), p, service.SubmitPlanInput{PlanID: r.PathValue("id"), ExpectedVersion: input.Version, ClusterIDs: input.ClusterIDs}, RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}
func (s *Server) approvePlan(w http.ResponseWriter, r *http.Request) {
	s.planVersionAction(w, r, s.dispatch.Approve)
}
func (s *Server) dispatchPlan(w http.ResponseWriter, r *http.Request) {
	s.planVersionAction(w, r, s.dispatch.Dispatch)
}
func (s *Server) planVersionAction(w http.ResponseWriter, r *http.Request, action func(context.Context, service.Principal, string, int64, string) (dispatch.Plan, error)) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	plan, err := action(r.Context(), p, r.PathValue("id"), input.Version, RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}
func (s *Server) recordTelemetry(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input struct {
		ClusterID         string `json:"cluster_id"`
		Sequence          int64  `json:"sequence"`
		ObservedAt        string `json:"observed_at"`
		SOC               int    `json:"soc"`
		PowerKW           int64  `json:"power_kw"`
		TemperatureMilliC int64  `json:"temperature_milli_c"`
		EnergyDeltaWh     int64  `json:"energy_delta_wh"`
	}
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	observed, err := parseTime(input.ObservedAt)
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.telemetry.Record(r.Context(), p, telemetry.Reading{ClusterID: input.ClusterID, Sequence: input.Sequence, ObservedAt: observed, SOC: input.SOC, PowerKW: input.PowerKW, TemperatureMilliC: input.TemperatureMilliC, EnergyDeltaWh: input.EnergyDeltaWh}, RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (s *Server) acknowledgeAlarm(w http.ResponseWriter, r *http.Request) {
	s.alarmVersionAction(w, r, s.alarms.Acknowledge)
}
func (s *Server) resolveAlarm(w http.ResponseWriter, r *http.Request) {
	s.alarmVersionAction(w, r, s.alarms.Resolve)
}
func (s *Server) alarmVersionAction(w http.ResponseWriter, r *http.Request, action func(context.Context, service.Principal, string, int64, string) (alarm.Alarm, error)) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	entity, err := action(r.Context(), p, r.PathValue("id"), input.Version, RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entity)
}
func (s *Server) batchAcknowledge(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOrError(w, r)
	if !ok {
		return
	}
	var input struct {
		IDs      []string         `json:"ids"`
		Versions map[string]int64 `json:"versions"`
	}
	if err := decodeJSON(w, r, s.maxBytes, &input); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.alarms.AcknowledgeBatch(r.Context(), p, input.IDs, input.Versions, RequestID(r.Context())))
}
func queryInt(r *http.Request, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fault.New(fault.Invalid, "invalid_query_integer", name+" must be an integer")
	}
	return value, nil
}
