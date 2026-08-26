CREATE INDEX idx_plans_status_start ON dispatch_plans(status, starts_at);
CREATE INDEX idx_jobs_plan_status ON execution_jobs(plan_id, status);
CREATE INDEX idx_settlement_period_status ON settlement_periods(site_id, status, starts_at);
CREATE INDEX idx_idempotency_expiry ON idempotency_keys(expires_at, state);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at, revoked_at);
