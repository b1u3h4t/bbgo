package sqlite3

import (
	"github.com/c9s/rockhopper/v2"
)

// This migration was compiled from migrations/sqlite3/20260904103000_add_binance_futures_delivery_klines.sql.
func init() {
	AddStatementMigration("main", 20260904103000, "migrations/sqlite3/20260904103000_add_binance_futures_delivery_klines.sql", false,
		[]rockhopper.Statement{
			{Direction: rockhopper.DirectionUp, SQL: "CREATE TABLE `binance_futures_klines`\n(\n    `gid`                    INTEGER PRIMARY KEY AUTOINCREMENT,\n    `exchange`               VARCHAR(10)    NOT NULL,\n    `start_time`             DATETIME(3)    NOT NULL,\n    `end_time`               DATETIME(3)    NOT NULL,\n    `interval`               VARCHAR(3)     NOT NULL,\n    `symbol`                 VARCHAR(32)    NOT NULL,\n    `open`                   DECIMAL(16, 8) NOT NULL,\n    `high`                   DECIMAL(16, 8) NOT NULL,\n    `low`                    DECIMAL(16, 8) NOT NULL,\n    `close`                  DECIMAL(16, 8) NOT NULL DEFAULT 0.0,\n    `volume`                 DECIMAL(16, 8) NOT NULL DEFAULT 0.0,\n    `closed`                 BOOLEAN        NOT NULL DEFAULT TRUE,\n    `last_trade_id`          INT            NOT NULL DEFAULT 0,\n    `num_trades`             INT            NOT NULL DEFAULT 0,\n    `quote_volume`           DECIMAL        NOT NULL DEFAULT 0.0,\n    `taker_buy_base_volume`  DECIMAL        NOT NULL DEFAULT 0.0,\n    `taker_buy_quote_volume` DECIMAL        NOT NULL DEFAULT 0.0\n);"},
			{Direction: rockhopper.DirectionUp, SQL: "CREATE UNIQUE INDEX idx_kline_binance_futures_unique\n    ON binance_futures_klines (`symbol`, `interval`, `start_time`);"},
			{Direction: rockhopper.DirectionUp, SQL: "CREATE INDEX `binance_futures_klines_end_time_symbol_interval`\n    ON binance_futures_klines (`end_time`, `symbol`, `interval`);"},
			{Direction: rockhopper.DirectionUp, SQL: "CREATE TABLE `binance_delivery_klines`\n(\n    `gid`                    INTEGER PRIMARY KEY AUTOINCREMENT,\n    `exchange`               VARCHAR(10)    NOT NULL,\n    `start_time`             DATETIME(3)    NOT NULL,\n    `end_time`               DATETIME(3)    NOT NULL,\n    `interval`               VARCHAR(3)     NOT NULL,\n    `symbol`                 VARCHAR(32)    NOT NULL,\n    `open`                   DECIMAL(16, 8) NOT NULL,\n    `high`                   DECIMAL(16, 8) NOT NULL,\n    `low`                    DECIMAL(16, 8) NOT NULL,\n    `close`                  DECIMAL(16, 8) NOT NULL DEFAULT 0.0,\n    `volume`                 DECIMAL(16, 8) NOT NULL DEFAULT 0.0,\n    `closed`                 BOOLEAN        NOT NULL DEFAULT TRUE,\n    `last_trade_id`          INT            NOT NULL DEFAULT 0,\n    `num_trades`             INT            NOT NULL DEFAULT 0,\n    `quote_volume`           DECIMAL        NOT NULL DEFAULT 0.0,\n    `taker_buy_base_volume`  DECIMAL        NOT NULL DEFAULT 0.0,\n    `taker_buy_quote_volume` DECIMAL        NOT NULL DEFAULT 0.0\n);"},
			{Direction: rockhopper.DirectionUp, SQL: "CREATE UNIQUE INDEX idx_kline_binance_delivery_unique\n    ON binance_delivery_klines (`symbol`, `interval`, `start_time`);"},
			{Direction: rockhopper.DirectionUp, SQL: "CREATE INDEX `binance_delivery_klines_end_time_symbol_interval`\n    ON binance_delivery_klines (`end_time`, `symbol`, `interval`);"},
		},
		[]rockhopper.Statement{
			{Direction: rockhopper.DirectionDown, SQL: "DROP TABLE IF EXISTS binance_delivery_klines;"},
			{Direction: rockhopper.DirectionDown, SQL: "DROP TABLE IF EXISTS binance_futures_klines;"},
		},
	)
}
