package service

import (
	"errors"
	"testing"

	"github.com/lib/pq"
)

func TestSQLForDriver(t *testing.T) {
	in := "SELECT * FROM `trades` WHERE `symbol` = :symbol"
	if got := sqlForDriver("mysql", in); got != in {
		t.Fatalf("mysql must be unchanged: %q", got)
	}
	if got := sqlForDriver("sqlite3", in); got != in {
		t.Fatalf("sqlite3 must be unchanged: %q", got)
	}
	want := `SELECT * FROM "trades" WHERE "symbol" = :symbol`
	if got := sqlForDriver("postgres", in); got != want {
		t.Fatalf("postgres rewrite: got %q want %q", got, want)
	}
}

func TestIfNullExpr(t *testing.T) {
	if got := ifNullExpr("mysql", "a", "b"); got != "IFNULL(a, b)" {
		t.Fatalf("mysql: %s", got)
	}
	if got := ifNullExpr("postgres", "a", "b"); got != "COALESCE(a, b)" {
		t.Fatalf("postgres: %s", got)
	}
}

func TestIsDuplicateKeyErrorPostgres(t *testing.T) {
	err := &pq.Error{Code: "23505"}
	if !isDuplicateKeyError(err) {
		t.Fatal("expected postgres unique_violation to be duplicate")
	}
	if !isDuplicateKeyError(errors.New("ERROR: duplicate key value violates unique constraint")) {
		t.Fatal("expected message-based detection")
	}
	if isDuplicateKeyError(errors.New("connection refused")) {
		t.Fatal("non-duplicate must be false")
	}
}
