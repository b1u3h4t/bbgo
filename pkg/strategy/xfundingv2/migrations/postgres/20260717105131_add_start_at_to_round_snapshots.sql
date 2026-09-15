-- @package xfundingv2
-- +up
-- +begin
ALTER TABLE xfundingv2_round_snapshots ADD COLUMN started_at TIMESTAMPTZ(3) NULL;
-- +end

-- +down
-- +begin
ALTER TABLE xfundingv2_round_snapshots DROP COLUMN started_at;
-- +end
