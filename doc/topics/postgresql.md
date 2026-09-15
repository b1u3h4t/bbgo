# PostgreSQL support (opt-in)

bbgo can use PostgreSQL when `DB_DRIVER=postgres`. **Production MySQL is unchanged** —
Postgres is additive and only activates when explicitly configured.

## Requirements

- Rockhopper v2 postgres dialect (`github.com/c9s/rockhopper`)
- Migrations under `migrations/postgres` (compiled to `pkg/migrations/postgres`)
- `lib/pq` driver (blank-imported from `pkg/service`)

## Configure

```bash
DB_DRIVER=postgres
DB_DSN=postgres://bbgo:bbgo@127.0.0.1:5432/bbgo?sslmode=disable
# or POSTGRES_URL=...
```

On connect, bbgo runs rockhopper Upgrade with the postgres migration set.

## Safety

| Path | Behavior |
|------|----------|
| `DB_DRIVER=mysql` (prod) | Identical to before |
| `DB_DRIVER=sqlite3` | Identical to before |
| `DB_DRIVER=postgres` | New path only |

Trade/order inserts for non-MySQL already used plain UUID text + INSERT (no `UUID_TO_BIN`).
Postgres follows that path. Sync unique conflicts map MySQL 1062 and Postgres `23505`.

## Migrate existing MySQL data (separate ops step)

Application support ≠ data cutover. For a later production move:

1. Stand up Postgres and run bbgo once with `DB_DRIVER=postgres` to apply schema
2. Copy data with `pgloader` / custom export (watch UUID binary → text)
3. Validate row counts on trades/orders/klines
4. Switch `DB_DSN` only after validation; keep MySQL as rollback

Do **not** flip production until the data migration plan is rehearsed.

## CI

- **Self-hosted runners** (`nc2-bbgo`, `nc3-bbgo`, labels `self-hosted,bbgo`): native PostgreSQL 17 from Debian packages, listening on `127.0.0.1:5432` only. Credentials for the `github-runner` user are in `~/.bbgo-ci-postgres.env`. Each Go workflow run creates `bbgo_ci_<run_id>` and drops it on cleanup.
- **GitHub-hosted PRs**: `postgres:17` Docker on port 5432 (fork PRs must not use the private runners).
- Workflow `.github/workflows/go.yml` applies `migrations/postgres` via rockhopper, then runs `TestPostgresCISchema` and xfundingv2 round-insert tests against that database.

```bash
# local equivalent
rockhopper --config rockhopper_postgres.yaml up
DB_DRIVER=postgres DB_DSN='postgres://bbgo:bbgo@127.0.0.1:5432/bbgo?sslmode=disable' \
  go test ./pkg/service/ ./pkg/strategy/xfundingv2/ -run 'PostgresCISchema|RoundInsert'
```
