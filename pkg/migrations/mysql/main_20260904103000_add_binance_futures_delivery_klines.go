package mysql

import (
	"github.com/c9s/rockhopper/v2"
)

// This migration was compiled from migrations/mysql/20260904103000_add_binance_futures_delivery_klines.sql.
func init() {
	AddStatementMigration("main", 20260904103000, "migrations/mysql/20260904103000_add_binance_futures_delivery_klines.sql", true,
		[]rockhopper.Statement{
			{Direction: rockhopper.DirectionUp, SQL: "CREATE TABLE `binance_futures_klines` LIKE `binance_klines`;"},
			{Direction: rockhopper.DirectionUp, SQL: "ALTER TABLE `binance_futures_klines` MODIFY `symbol` VARCHAR(32) NOT NULL;"},
			{Direction: rockhopper.DirectionUp, SQL: "CREATE TABLE `binance_delivery_klines` LIKE `binance_klines`;"},
			{Direction: rockhopper.DirectionUp, SQL: "ALTER TABLE `binance_delivery_klines` MODIFY `symbol` VARCHAR(32) NOT NULL;"},
		},
		[]rockhopper.Statement{
			{Direction: rockhopper.DirectionDown, SQL: "DROP TABLE IF EXISTS `binance_delivery_klines`;"},
			{Direction: rockhopper.DirectionDown, SQL: "DROP TABLE IF EXISTS `binance_futures_klines`;"},
		},
	)
}
