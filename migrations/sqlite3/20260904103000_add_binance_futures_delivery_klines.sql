-- !txn
-- +up
-- +begin
CREATE TABLE `binance_futures_klines`
(
    `gid`                    INTEGER PRIMARY KEY AUTOINCREMENT,
    `exchange`               VARCHAR(10)    NOT NULL,
    `start_time`             DATETIME(3)    NOT NULL,
    `end_time`               DATETIME(3)    NOT NULL,
    `interval`               VARCHAR(3)     NOT NULL,
    `symbol`                 VARCHAR(32)    NOT NULL,
    `open`                   DECIMAL(16, 8) NOT NULL,
    `high`                   DECIMAL(16, 8) NOT NULL,
    `low`                    DECIMAL(16, 8) NOT NULL,
    `close`                  DECIMAL(16, 8) NOT NULL DEFAULT 0.0,
    `volume`                 DECIMAL(16, 8) NOT NULL DEFAULT 0.0,
    `closed`                 BOOLEAN        NOT NULL DEFAULT TRUE,
    `last_trade_id`          INT            NOT NULL DEFAULT 0,
    `num_trades`             INT            NOT NULL DEFAULT 0,
    `quote_volume`           DECIMAL        NOT NULL DEFAULT 0.0,
    `taker_buy_base_volume`  DECIMAL        NOT NULL DEFAULT 0.0,
    `taker_buy_quote_volume` DECIMAL        NOT NULL DEFAULT 0.0
);
-- +end

-- +begin
CREATE UNIQUE INDEX idx_kline_binance_futures_unique
    ON binance_futures_klines (`symbol`, `interval`, `start_time`);
-- +end

-- +begin
CREATE INDEX `binance_futures_klines_end_time_symbol_interval`
    ON binance_futures_klines (`end_time`, `symbol`, `interval`);
-- +end

-- +begin
CREATE TABLE `binance_delivery_klines`
(
    `gid`                    INTEGER PRIMARY KEY AUTOINCREMENT,
    `exchange`               VARCHAR(10)    NOT NULL,
    `start_time`             DATETIME(3)    NOT NULL,
    `end_time`               DATETIME(3)    NOT NULL,
    `interval`               VARCHAR(3)     NOT NULL,
    `symbol`                 VARCHAR(32)    NOT NULL,
    `open`                   DECIMAL(16, 8) NOT NULL,
    `high`                   DECIMAL(16, 8) NOT NULL,
    `low`                    DECIMAL(16, 8) NOT NULL,
    `close`                  DECIMAL(16, 8) NOT NULL DEFAULT 0.0,
    `volume`                 DECIMAL(16, 8) NOT NULL DEFAULT 0.0,
    `closed`                 BOOLEAN        NOT NULL DEFAULT TRUE,
    `last_trade_id`          INT            NOT NULL DEFAULT 0,
    `num_trades`             INT            NOT NULL DEFAULT 0,
    `quote_volume`           DECIMAL        NOT NULL DEFAULT 0.0,
    `taker_buy_base_volume`  DECIMAL        NOT NULL DEFAULT 0.0,
    `taker_buy_quote_volume` DECIMAL        NOT NULL DEFAULT 0.0
);
-- +end

-- +begin
CREATE UNIQUE INDEX idx_kline_binance_delivery_unique
    ON binance_delivery_klines (`symbol`, `interval`, `start_time`);
-- +end

-- +begin
CREATE INDEX `binance_delivery_klines_end_time_symbol_interval`
    ON binance_delivery_klines (`end_time`, `symbol`, `interval`);
-- +end

-- +down

-- +begin
DROP TABLE IF EXISTS binance_delivery_klines;
-- +end

-- +begin
DROP TABLE IF EXISTS binance_futures_klines;
-- +end
