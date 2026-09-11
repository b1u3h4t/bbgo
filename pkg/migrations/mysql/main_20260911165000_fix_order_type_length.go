package mysql

import (
	"github.com/c9s/rockhopper/v2"
)

// This migration was compiled from migrations/mysql/20260911165000_fix_order_type_length.sql.
// The SQL statements are registered as data so they can be previewed in the
// console while the migration runs, exactly like a raw .sql migration.
func init() {
	AddStatementMigration("main", 20260911165000, "migrations/mysql/20260911165000_fix_order_type_length.sql", true,
		[]rockhopper.Statement{
			{Direction: rockhopper.DirectionUp, SQL: "ALTER TABLE `orders`\n    CHANGE `order_type` `order_type` varchar(32) NOT NULL;"},
		},
		[]rockhopper.Statement{
			{Direction: rockhopper.DirectionDown, SQL: "ALTER TABLE `orders`\n    CHANGE `order_type` `order_type` varchar(16) NOT NULL;"},
		},
	)
}
