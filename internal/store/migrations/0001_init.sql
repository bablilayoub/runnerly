-- Initial Runnerly control plane schema.
--
-- The project plan lists more tables than this. Several are deliberately not
-- created yet: organizations, github_connections, repositories, runner_labels
-- and runner_installations all describe relationships that a single column
-- answers today, and creating them now would be normalizing ahead of a
-- concrete need.

-- Runners known to the control plane.
--
-- A runner is identified to GitHub by (host, scope, name), so that triple is
-- unique. github_runner_id is GitHub's own id, which is only known once the
-- runner has registered there.
CREATE TABLE runners (
    id               UUID PRIMARY KEY,
    name             TEXT        NOT NULL,
    github_host      TEXT        NOT NULL DEFAULT 'github.com',
    github_scope     TEXT        NOT NULL CHECK (github_scope IN ('repository', 'organization')),
    github_scope_id  TEXT        NOT NULL,
    github_runner_id BIGINT,

    -- Status as last reported by the agent. Whether it is still true depends
    -- on last_heartbeat, which the server compares against its thresholds
    -- rather than writing a stale value into this column.
    status           TEXT        NOT NULL DEFAULT 'offline'
                     CHECK (status IN ('offline', 'starting', 'online', 'busy', 'stopping', 'error')),
    status_detail    TEXT        NOT NULL DEFAULT '',

    os               TEXT        NOT NULL DEFAULT '',
    architecture     TEXT        NOT NULL DEFAULT '',
    cpu_count        INTEGER     NOT NULL DEFAULT 0,
    memory_bytes     BIGINT      NOT NULL DEFAULT 0,
    disk_bytes       BIGINT      NOT NULL DEFAULT 0,

    labels           TEXT[]      NOT NULL DEFAULT '{}',
    ephemeral        BOOLEAN     NOT NULL DEFAULT FALSE,

    runner_version   TEXT        NOT NULL DEFAULT '',
    agent_version    TEXT        NOT NULL DEFAULT '',

    -- Most recent sample from a heartbeat. History would need a time series
    -- table, which is a larger commitment than v1 needs.
    cpu_percent      DOUBLE PRECISION NOT NULL DEFAULT 0,
    memory_percent   DOUBLE PRECISION NOT NULL DEFAULT 0,
    disk_percent     DOUBLE PRECISION NOT NULL DEFAULT 0,

    last_heartbeat   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (github_host, github_scope_id, name)
);

CREATE INDEX runners_scope_idx ON runners (github_host, github_scope_id);
CREATE INDEX runners_heartbeat_idx ON runners (last_heartbeat);

-- Things that happened to a runner, for the dashboard's event feed.
CREATE TABLE runner_events (
    id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    runner_id  UUID        REFERENCES runners (id) ON DELETE CASCADE,
    event      TEXT        NOT NULL,
    severity   TEXT        NOT NULL DEFAULT 'info'
               CHECK (severity IN ('debug', 'info', 'warn', 'error')),
    message    TEXT        NOT NULL DEFAULT '',
    -- Anything the agent wants to attach; the shape varies by event.
    data       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX runner_events_runner_idx ON runner_events (runner_id, created_at DESC);
CREATE INDEX runner_events_created_idx ON runner_events (created_at DESC);

-- One-shot tokens an operator issues so a machine can enroll itself.
--
-- Only the hash is stored. A bearer credential does not need to be readable
-- back, and a hash cannot be leaked from a database dump.
CREATE TABLE enrollment_tokens (
    id          UUID PRIMARY KEY,
    token_hash  BYTEA       NOT NULL UNIQUE,
    description TEXT        NOT NULL DEFAULT '',
    -- Null means the token may enroll any number of machines.
    max_uses    INTEGER,
    uses        INTEGER     NOT NULL DEFAULT 0,
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The credential an enrolled agent uses from then on. Rotating one issues a
-- new row and revokes the old, so a rotation is auditable.
CREATE TABLE machine_tokens (
    id           UUID PRIMARY KEY,
    runner_id    UUID        NOT NULL REFERENCES runners (id) ON DELETE CASCADE,
    token_hash   BYTEA       NOT NULL UNIQUE,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX machine_tokens_runner_idx ON machine_tokens (runner_id);

-- Dashboard accounts, created on first GitHub sign-in.
CREATE TABLE users (
    id            UUID PRIMARY KEY,
    github_id     BIGINT      NOT NULL UNIQUE,
    login         TEXT        NOT NULL,
    name          TEXT        NOT NULL DEFAULT '',
    avatar_url    TEXT        NOT NULL DEFAULT '',
    -- The user's GitHub token, encrypted with the server key. It is a
    -- credential the server must be able to use, so unlike bearer tokens it
    -- is encrypted rather than hashed.
    access_token  BYTEA,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Dashboard sessions. The cookie carries a random value; only its hash is
-- stored, for the same reason as the other bearer credentials.
CREATE TABLE sessions (
    id           UUID PRIMARY KEY,
    user_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   BYTEA       NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expiry_idx ON sessions (expires_at);

-- Who did what. Kept deliberately simple: an actor, an action, and the thing
-- it happened to.
CREATE TABLE audit_logs (
    id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor      TEXT        NOT NULL,
    action     TEXT        NOT NULL,
    target     TEXT        NOT NULL DEFAULT '',
    detail     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_created_idx ON audit_logs (created_at DESC);
