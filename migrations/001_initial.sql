CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('dispatcher', 'operator', 'auditor')),
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    last_seen_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_user_active ON sessions(user_id, expires_at, revoked_at);

CREATE TABLE sites (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    timezone TEXT NOT NULL,
    grid_limit_kw INTEGER NOT NULL CHECK (grid_limit_kw > 0),
    reserved_kw INTEGER NOT NULL DEFAULT 0 CHECK (reserved_kw >= 0),
    status TEXT NOT NULL CHECK (status IN ('commissioning', 'active', 'suspended', 'retired')),
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (reserved_kw <= grid_limit_kw)
);

CREATE TABLE battery_clusters (
    id TEXT PRIMARY KEY,
    site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    code TEXT NOT NULL,
    rated_power_kw INTEGER NOT NULL CHECK (rated_power_kw > 0),
    capacity_kwh INTEGER NOT NULL CHECK (capacity_kwh > 0),
    min_soc INTEGER NOT NULL CHECK (min_soc BETWEEN 0 AND 100),
    max_soc INTEGER NOT NULL CHECK (max_soc BETWEEN 0 AND 100),
    current_soc INTEGER NOT NULL CHECK (current_soc BETWEEN 0 AND 100),
    status TEXT NOT NULL CHECK (status IN ('available', 'reserved', 'running', 'degraded', 'offline')),
    latest_sequence INTEGER NOT NULL DEFAULT 0,
    latest_telemetry_at TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(site_id, code),
    CHECK (min_soc < max_soc)
);
CREATE INDEX idx_clusters_site_status ON battery_clusters(site_id, status);

CREATE TABLE dispatch_plans (
    id TEXT PRIMARY KEY,
    site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('charge', 'discharge')),
    requested_kw INTEGER NOT NULL CHECK (requested_kw > 0),
    target_kwh INTEGER NOT NULL CHECK (target_kwh > 0),
    starts_at TEXT NOT NULL,
    ends_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'submitted', 'approved', 'dispatched', 'running', 'completed', 'failed', 'cancelled')),
    created_by TEXT NOT NULL REFERENCES users(id),
    approved_by TEXT REFERENCES users(id),
    approved_at TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (starts_at < ends_at)
);
CREATE INDEX idx_plans_site_window ON dispatch_plans(site_id, starts_at, ends_at, status);

CREATE TABLE capacity_reservations (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES dispatch_plans(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES battery_clusters(id) ON DELETE RESTRICT,
    reserved_kw INTEGER NOT NULL CHECK (reserved_kw > 0),
    starts_at TEXT NOT NULL,
    ends_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('held', 'consumed', 'released')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(plan_id, cluster_id)
);
CREATE INDEX idx_reservations_cluster_window ON capacity_reservations(cluster_id, starts_at, ends_at, status);

CREATE TABLE execution_jobs (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES dispatch_plans(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES battery_clusters(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'leased', 'succeeded', 'retryable', 'permanent_failure', 'cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL CHECK (max_attempts > 0),
    next_attempt_at TEXT NOT NULL,
    lease_owner TEXT,
    lease_until TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    command_key TEXT NOT NULL UNIQUE,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(plan_id, cluster_id)
);
CREATE INDEX idx_jobs_claim ON execution_jobs(status, next_attempt_at, lease_until);

CREATE TABLE telemetry_snapshots (
    id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL REFERENCES battery_clusters(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL,
    observed_at TEXT NOT NULL,
    soc INTEGER NOT NULL CHECK (soc BETWEEN 0 AND 100),
    power_kw INTEGER NOT NULL,
    temperature_milli_c INTEGER NOT NULL,
    energy_delta_wh INTEGER NOT NULL,
    received_at TEXT NOT NULL,
    UNIQUE(cluster_id, sequence)
);
CREATE INDEX idx_telemetry_cluster_time ON telemetry_snapshots(cluster_id, observed_at DESC);

CREATE TABLE alarms (
    id TEXT PRIMARY KEY,
    site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    cluster_id TEXT NOT NULL REFERENCES battery_clusters(id) ON DELETE RESTRICT,
    alarm_type TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('warning', 'critical')),
    status TEXT NOT NULL CHECK (status IN ('open', 'acknowledged', 'resolved')),
    fingerprint TEXT NOT NULL,
    message TEXT NOT NULL,
    opened_at TEXT NOT NULL,
    acknowledged_by TEXT REFERENCES users(id),
    acknowledged_at TEXT,
    resolved_by TEXT REFERENCES users(id),
    resolved_at TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_alarm_active_fingerprint ON alarms(fingerprint) WHERE status <> 'resolved';
CREATE INDEX idx_alarms_site_status ON alarms(site_id, status, opened_at DESC);

CREATE TABLE settlement_periods (
    id TEXT PRIMARY KEY,
    site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    starts_at TEXT NOT NULL,
    ends_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('open', 'calculating', 'closed')),
    closed_by TEXT REFERENCES users(id),
    closed_at TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(site_id, starts_at, ends_at),
    CHECK (starts_at < ends_at)
);

CREATE TABLE settlement_entries (
    id TEXT PRIMARY KEY,
    period_id TEXT NOT NULL REFERENCES settlement_periods(id) ON DELETE CASCADE,
    plan_id TEXT NOT NULL REFERENCES dispatch_plans(id) ON DELETE RESTRICT,
    planned_wh INTEGER NOT NULL,
    actual_wh INTEGER NOT NULL,
    deviation_wh INTEGER NOT NULL,
    amount_milli_cent INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(period_id, plan_id)
);

CREATE TABLE idempotency_keys (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    key_value TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_status INTEGER,
    response_body BLOB,
    state TEXT NOT NULL CHECK (state IN ('started', 'completed')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(actor_id, method, path, key_value)
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    request_id TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    action TEXT NOT NULL,
    result TEXT NOT NULL,
    details_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL
);
CREATE INDEX idx_audit_object ON audit_events(object_type, object_id, occurred_at DESC, id DESC);
CREATE INDEX idx_audit_request ON audit_events(request_id);

CREATE TABLE worker_leases (
    name TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    lease_until TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);
