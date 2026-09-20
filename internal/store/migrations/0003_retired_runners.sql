-- Ephemeral runners finish and are taken apart. Without a way to say so, a
-- retired runner is indistinguishable from one whose machine fell over: both
-- are simply "offline", and a dashboard fills up with rows that look like
-- failures.
--
-- The row is kept rather than deleted, because what a runner did is worth
-- more than the row costs, and its events reference it.
ALTER TABLE runners
    ADD COLUMN retired_at TIMESTAMPTZ,
    ADD COLUMN retired_reason TEXT NOT NULL DEFAULT '';

-- The common listing is "everything still in service".
CREATE INDEX runners_live_idx ON runners (created_at DESC) WHERE retired_at IS NULL;
