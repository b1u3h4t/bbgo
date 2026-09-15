package service

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

// TestPostgresCISchema is an opt-in smoke test for CI / local Postgres.
// It skips unless DB_DRIVER=postgres and DB_DSN is set (migrations already applied).
func TestPostgresCISchema(t *testing.T) {
	if os.Getenv("DB_DRIVER") != "postgres" {
		t.Skip("DB_DRIVER is not postgres")
	}
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN is not set")
	}

	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var n int
	require.NoError(t, db.Get(&n, "SELECT COUNT(*) FROM rockhopper_versions"))
	require.Greater(t, n, 0, "postgres migrations should already be applied")

	for _, table := range []string{"trades", "orders", "binance_futures_klines", "xfundingv2_closed_rounds"} {
		var exists bool
		err := db.Get(&exists, `SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)`, table)
		require.NoError(t, err)
		require.True(t, exists, "expected table %s", table)
	}
}
