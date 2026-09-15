-- @package xfundingv2
-- +up
-- +begin
CREATE TABLE xfundingv2_closed_rounds
(
 gid BIGSERIAL PRIMARY KEY,

 id TEXT NOT NULL DEFAULT '',
 strategy_instance_id TEXT NOT NULL,
 spot_symbol TEXT NOT NULL,
 futures_symbol TEXT NOT NULL,
 spot_exchange TEXT NOT NULL DEFAULT '',
 futures_exchange TEXT NOT NULL DEFAULT '',
 direction TEXT NOT NULL DEFAULT '',
 collateral_asset TEXT NOT NULL DEFAULT '',

 leverage INTEGER NOT NULL DEFAULT 0,
 triggered_funding_rate REAL NOT NULL DEFAULT 0,
 annualized_rate REAL NOT NULL DEFAULT 0,
 funding_income REAL NOT NULL DEFAULT 0,

 spot_pnl REAL NOT NULL DEFAULT 0,
 spot_net_pnl REAL NOT NULL DEFAULT 0,
 futures_pnl REAL NOT NULL DEFAULT 0,
 futures_net_pnl REAL NOT NULL DEFAULT 0,
 net_pnl REAL NOT NULL DEFAULT 0,

 num_holding_intervals INTEGER NOT NULL DEFAULT 0,

 start_at TIMESTAMPTZ(3) NOT NULL,
 ready_at TIMESTAMPTZ(3) NULL,
 closing_at TIMESTAMPTZ(3) NOT NULL,
 closed_at TIMESTAMPTZ(3) NOT NULL,

 inserted_at TIMESTAMPTZ(3) DEFAULT CURRENT_TIMESTAMP NOT NULL
);
-- +end

-- +begin
CREATE INDEX idx_xfundingv2_closed_rounds_instance ON xfundingv2_closed_rounds (strategy_instance_id);
-- +end

-- +begin
CREATE INDEX idx_xfundingv2_closed_rounds_round_id ON xfundingv2_closed_rounds (id);
-- +end

-- +begin
CREATE TABLE xfundingv2_funding_fees
(
 gid BIGSERIAL PRIMARY KEY,

 round_id TEXT NOT NULL DEFAULT '',
 asset TEXT NOT NULL DEFAULT '',
 amount REAL NOT NULL DEFAULT 0,
 txn INTEGER NOT NULL DEFAULT 0,
 "time" TIMESTAMPTZ NOT NULL
);
-- +end

-- +begin
CREATE INDEX idx_xfundingv2_funding_fees_round_id ON xfundingv2_funding_fees (round_id);
-- +end

-- +down
-- +begin
DROP TABLE IF EXISTS xfundingv2_funding_fees;
-- +end

-- +begin
DROP TABLE IF EXISTS xfundingv2_closed_rounds;
-- +end
