package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// sqlForDriver rewrites MySQL/SQLite-style backtick identifiers for Postgres.
// Production MySQL/SQLite paths pass SQL through unchanged.
func sqlForDriver(driver, sql string) string {
	if driver == "postgres" {
		return strings.ReplaceAll(sql, "`", `"`)
	}
	return sql
}

// intervalColumn returns a safely quoted interval column name for the driver.
func intervalColumn(driver string) string {
	if driver == "postgres" {
		return `"interval"`
	}
	return "`interval`"
}

// ifNullExpr returns IFNULL for mysql/sqlite and COALESCE for postgres.
func ifNullExpr(driver, expr, fallback string) string {
	if driver == "postgres" {
		return fmt.Sprintf("COALESCE(%s, %s)", expr, fallback)
	}
	return fmt.Sprintf("IFNULL(%s, %s)", expr, fallback)
}

// isDuplicateKeyError detects unique-violation for MySQL and Postgres.
// Existing MySQL behavior is preserved; Postgres is additive.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if isMysqlDuplicateError(err) {
		return true
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == "23505" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
