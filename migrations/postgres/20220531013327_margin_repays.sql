-- !txn
-- +up
CREATE TABLE margin_repays
(
 gid BIGSERIAL PRIMARY KEY,

 transaction_id INTEGER NOT NULL,

 exchange VARCHAR(24) NOT NULL DEFAULT '',

 asset VARCHAR(24) NOT NULL DEFAULT '',

 isolated_symbol VARCHAR(24) NOT NULL DEFAULT '',

 -- quantity is the quantity of the trade that makes profit
 principle DECIMAL(16, 8) NOT NULL,

 "time" TIMESTAMPTZ(3) NOT NULL
);

-- +down
DROP TABLE IF EXISTS margin_repays;
