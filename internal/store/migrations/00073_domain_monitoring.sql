-- +goose Up
-- Website reachability monitoring for managed application domains (GOTHAM-87).
-- The control plane probes each enabled managed domain through its public
-- host; host and container metrics remain agent-pushed.
--
-- Settings are separate from application_domains so an untouched domain is
-- monitored by default without a backfill. Results keep categorical outcomes
-- and the valid observation interval they account for; unknown gaps (process
-- downtime, disabled periods, grace windows) are never stored as samples.
CREATE TABLE domain_monitor_settings (
    domain_id uuid PRIMARY KEY REFERENCES application_domains(id) ON DELETE CASCADE,
    enabled boolean NOT NULL DEFAULT true,
    path text NOT NULL DEFAULT '/',
    expected_min integer NOT NULL DEFAULT 200
        CHECK (expected_min BETWEEN 100 AND 599),
    expected_max integer NOT NULL DEFAULT 399
        CHECK (expected_max BETWEEN 100 AND 599),
    timeout_seconds integer NOT NULL DEFAULT 5
        CHECK (timeout_seconds BETWEEN 1 AND 30),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT domain_monitor_settings_range_check
        CHECK (expected_min <= expected_max)
);

CREATE TABLE domain_monitor_state (
    domain_id uuid PRIMARY KEY REFERENCES application_domains(id) ON DELETE CASCADE,
    -- Confirmation is counted in completed probes, not wall time: three
    -- consecutive failures open, two consecutive successes recover.
    consecutive_failures integer NOT NULL DEFAULT 0,
    consecutive_successes integer NOT NULL DEFAULT 0,
    last_outcome text NOT NULL DEFAULT 'unknown'
        CHECK (last_outcome IN ('healthy', 'failed', 'unsupported', 'unknown')),
    last_reason text NOT NULL DEFAULT '',
    last_status integer,
    last_latency_ms integer,
    last_checked_at timestamptz,
    last_completed_at timestamptz,
    last_success_at timestamptz,
    last_failure_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE domain_probe_results (
    id bigserial PRIMARY KEY,
    domain_id uuid NOT NULL REFERENCES application_domains(id) ON DELETE CASCADE,
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    observed_at timestamptz NOT NULL,
    -- interval_start is the previous completed probe; valid_seconds is the
    -- fresh time this result may account for (0 across an unknown gap).
    interval_start timestamptz NOT NULL,
    outcome text NOT NULL
        CHECK (outcome IN ('healthy', 'failed', 'unsupported')),
    reason text NOT NULL DEFAULT '',
    status_code integer,
    latency_ms integer,
    valid_seconds integer NOT NULL DEFAULT 0
        CHECK (valid_seconds >= 0)
);

CREATE INDEX domain_probe_results_domain_time_idx
    ON domain_probe_results (domain_id, observed_at DESC);
CREATE INDEX domain_probe_results_retention_idx
    ON domain_probe_results (observed_at);

-- +goose Down
DROP TABLE IF EXISTS domain_probe_results;
DROP TABLE IF EXISTS domain_monitor_state;
DROP TABLE IF EXISTS domain_monitor_settings;
