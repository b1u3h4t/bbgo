#!/usr/bin/env bash
# Per-run temporary directory for bbgo CI on shared self-hosted runners (nc2/nc3).
#
# Layout:  $HOME/.cache/bbgo-ci-tmp/run-<run_id>-<job>-<attempt>/
#
# Safe by design — cleanup ONLY deletes that prefix. Never touches:
#   - other projects (e.g. avalanchego ~/actions-runner)
#   - shared tool caches (~/.cache/go-build, yarn, golangci-lint)
#   - module cache (~/go/pkg)
#   - /tmp contents belonging to concurrent jobs (no global /tmp sweep)
#
# Usage in workflows (after checkout):
#   - run: bash scripts/ci-bbgo-tmpdir.sh setup
#   - if: always()
#     run: bash scripts/ci-bbgo-tmpdir.sh cleanup
set -euo pipefail

cmd="${1:?usage: $0 setup|cleanup}"

base="${HOME}/.cache/bbgo-ci-tmp"
job="${GITHUB_JOB:-job}"
job="${job//\//-}"
job="${job// /_}"
attempt="${GITHUB_RUN_ATTEMPT:-1}"
run_id="${GITHUB_RUN_ID:-local}"
run_dir="${base}/run-${run_id}-${job}-${attempt}"

# Allowlist: must live under ~/.cache/bbgo-ci-tmp/run-*
is_safe_bbgo_tmp() {
  local p="$1"
  case "$p" in
    "${HOME}/.cache/bbgo-ci-tmp/run-"*) return 0 ;;
    *) return 1 ;;
  esac
}

case "$cmd" in
  setup)
    mkdir -p "$run_dir"
    if [[ -n "${GITHUB_ENV:-}" ]]; then
      {
        echo "BBGO_CI_TMP=${run_dir}"
        echo "TMPDIR=${run_dir}"
        echo "GOTMPDIR=${run_dir}"
        echo "TMP=${run_dir}"
        echo "TEMP=${run_dir}"
      } >>"$GITHUB_ENV"
    fi
    echo "bbgo CI tmpdir ready: ${run_dir}"
    ;;

  cleanup)
    target="${BBGO_CI_TMP:-}"
    if [[ -n "$target" ]]; then
      if is_safe_bbgo_tmp "$target"; then
        if [[ -e "$target" ]]; then
          echo "removing bbgo CI tmpdir: ${target}"
          rm -rf -- "$target"
        fi
      else
        echo "refusing cleanup of unexpected BBGO_CI_TMP=${target} (not under ${base}/run-*)" >&2
      fi
    fi

    # Orphans from crashed jobs under this prefix only (mtime > 2 days).
    if [[ -d "$base" ]]; then
      while IFS= read -r -d '' d; do
        if is_safe_bbgo_tmp "$d"; then
          echo "pruning stale bbgo CI tmpdir: ${d}"
          rm -rf -- "$d"
        fi
      done < <(find "$base" -mindepth 1 -maxdepth 1 -type d -name 'run-*' -mtime +2 -print0 2>/dev/null || true)
    fi
    ;;

  *)
    echo "usage: $0 setup|cleanup" >&2
    exit 2
    ;;
esac
