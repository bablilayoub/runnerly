-- Commands the control plane asks an agent to carry out.
--
-- The server cannot reach an agent: agents live on machines behind NAT and
-- firewalls, and opening an inbound port on every runner would be a worse
-- idea than waiting. Commands are therefore queued here and handed to the
-- agent on its next heartbeat, which means "restart" takes effect within one
-- heartbeat interval rather than instantly. The dashboard says so instead of
-- pretending the button was immediate.
CREATE TABLE runner_commands (
    id           UUID PRIMARY KEY,
    runner_id    UUID        NOT NULL REFERENCES runners (id) ON DELETE CASCADE,
    command      TEXT        NOT NULL CHECK (command IN ('restart')),

    status       TEXT        NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'delivered', 'done', 'failed')),
    -- Set when the command finished, successfully or not.
    error        TEXT        NOT NULL DEFAULT '',

    requested_by TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

-- The hot path is "what is waiting for this runner", so index exactly that.
CREATE INDEX runner_commands_pending_idx
    ON runner_commands (runner_id, created_at)
    WHERE status IN ('pending', 'delivered');

CREATE INDEX runner_commands_runner_idx ON runner_commands (runner_id, created_at DESC);
