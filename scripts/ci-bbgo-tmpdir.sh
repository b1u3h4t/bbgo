#!/usr/bin/env bash
# Per-run temporary directory for bbgo CI on shared self-hosted runners (nc2/nc3).
#
# Layout:  $HOME/.cache/bbgo-ci-tmp/run-<run_id>-<job>-<attempt>/
#
# Safe by design — cleanup ONLY deletes that prefix + bbgo-ci-* Docker names.
# Never touches:
#   - other projects' containers/processes (postgres, flaresolverr, etc.)
#   - other projects (e.g. avalanchego ~/actions-runner)
#   - shared tool caches (~/.cache/go-build, yarn, golangci-lint)
#   - module cache (~/go/pkg)
#   - /tmp contents belonging to concurrent jobs (no global /tmp sweep)
#
# Usage in workflows (after checkout):
#   - run: bash scripts/ci-bbgo-tmpdir.sh setup
#   - run: bash scripts/ci-bbgo-tmpdir.sh preflight   # optional disk guard
#   - if: always()
#     run: bash scripts/ci-bbgo-tmpdir.sh cleanup
set -euo pipefail

cmd="${1:?usage: $0 setup|cleanup|preflight}"

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

# Only bbgo CI docker names — never other host workloads.
rm_bbgo_ci_containers() {
  if ! command -v docker >/dev/null 2>&1; then
    return 0
  fi
  local ids
  ids="$(docker ps -aq --filter name=bbgo-ci- 2>/dev/null || true)"
  if [[ -n "$ids" ]]; then
    echo "removing bbgo-ci-* containers only:"
    # shellcheck disable=SC2086
    docker ps -a --filter name=bbgo-ci- --format '  {{.Names}} {{.Status}}' || true
    # shellcheck disable=SC2086
    docker rm -f $ids >/dev/null 2>&1 || true
  fi
}

disk_free_kib() {
  # Portable: df -Pk on the HOME filesystem.
  df -Pk "${HOME:-/}" 2>/dev/null | awk 'NR==2 {print $4}'
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

  preflight)
    echo "==> disk (HOME filesystem)"
    df -h "${HOME:-/}" || df -h /
    rm_bbgo_ci_containers
    # Drop orphaned per-run tmpdirs from crashed/cancelled jobs (any age except current).
    if [[ -d "$base" ]]; then
      while IFS= read -r -d '' d; do
        if [[ "$d" == "$run_dir" ]]; then
          continue
        fi
        if is_safe_bbgo_tmp "$d"; then
          echo "pruning leftover bbgo CI tmpdir: ${d}"
          rm -rf -- "$d"
        fi
      done < <(find "$base" -mindepth 1 -maxdepth 1 -type d -name 'run-*' -print0 2>/dev/null || true)
    fi
    free_kib="$(disk_free_kib || echo 0)"
    # Require ~5 GiB free so the runner can write _diag / work without ENOSPC.
    need_kib=$((5 * 1024 * 1024))
    echo "free_kib=${free_kib} need_kib=${need_kib}"
    if [[ "${free_kib:-0}" -lt "$need_kib" ]]; then
      echo "::error::self-hosted runner disk low (${free_kib} KiB free); free space before re-running (bbgo-ci-* only cleaned; other host workloads untouched)" >&2
      exit 1
    fi
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
    rm_bbgo_ci_containers
    ;;

  *)
    echo "usage: $0 setup|cleanup|preflight" >&2
    exit 2
    ;;
esac
