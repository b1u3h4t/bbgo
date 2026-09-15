package postgres

import (
	"github.com/c9s/rockhopper/v2"
)

// This migration was compiled from migrations/postgres/20240531163411_trades_created.sql.
// The SQL statements are registered as data so they can be previewed in the
// console while the migration runs, exactly like a raw .sql migration.
func init() {
	AddStatementMigration("main", 20240531163411, "migrations/postgres/20240531163411_trades_created.sql", true,
		[]rockhopper.Statement{
			{Direction: rockhopper.DirectionUp, SQL: "ALTER TABLE trades ADD COLUMN inserted_at TIMESTAMPTZ;\nUPDATE trades SET inserted_at = traded_at;\nCREATE OR REPLACE FUNCTION trades_set_inserted_at() RETURNS trigger AS $$\nBEGIN\n  IF NEW.inserted_at IS NULL THEN\n    NEW.inserted_at := NOW();\n  END IF;\n  RETURN NEW;\nEND;\n$$ LANGUAGE plpgsql;\nCREATE TRIGGER set_inserted_at\nBEFORE INSERT ON trades\nFOR EACH ROW\nEXECUTE PROCEDURE trades_set_inserted_at();"},
		},
		[]rockhopper.Statement{
			{Direction: rockhopper.DirectionDown, SQL: "DROP TRIGGER IF EXISTS set_inserted_at ON trades;\nDROP FUNCTION IF EXISTS trades_set_inserted_at();\nALTER TABLE trades DROP COLUMN IF EXISTS inserted_at;"},
		},
	)
}
