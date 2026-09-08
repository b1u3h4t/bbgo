#!/usr/bin/env bash
# Run ON the production host (bbgo) to swap the trading binary safely.
#
# Usage:
#   scripts/deploy-prod-remote.sh /path/to/new-bbgo-binary [git-sha]
#
# Expects:
#   - systemd unit "bbgo.service"
#   - binary at $BBGO_HOME/bbgo (default /home/admin/bbgo/bbgo)
#   - passwordless sudo for systemctl
set -euo pipefail

NEW_BIN=${1:-}
GIT_SHA=${2:-unknown}
BBGO_HOME=${BBGO_HOME:-/home/admin/bbgo}
SERVICE=${BBGO_SERVICE:-bbgo}
LOCK_FILE=${BBGO_DEPLOY_LOCK:-/tmp/bbgo-deploy.lock}
HEALTH_URL=${BBGO_HEALTH_URL:-http://127.0.0.1:8080/}
STOP_TIMEOUT=${BBGO_STOP_TIMEOUT:-100}
START_TIMEOUT=${BBGO_START_TIMEOUT:-60}

if [[ -z "$NEW_BIN" || ! -f "$NEW_BIN" ]]; then
  echo "usage: $0 /path/to/new-binary [git-sha]" >&2
  exit 2
fi

if [[ ! -x "$NEW_BIN" ]]; then
  chmod +x "$NEW_BIN"
fi

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "another deploy is in progress (lock $LOCK_FILE)" >&2
  exit 1
fi

ts=$(date -u +%Y%m%d%H%M%S)
current="$BBGO_HOME/bbgo"
backup="$BBGO_HOME/bbgo.bak.$ts"
staging="$BBGO_HOME/bbgo.new.$ts"

echo "==> deploy $GIT_SHA -> $current"
install -m 0755 "$NEW_BIN" "$staging"

# Refuse if an unexpected second bbgo process is already running outside systemd
running=$(pgrep -u "$(id -un)" -x bbgo | wc -l | tr -d ' ')
if [[ "$running" -gt 1 ]]; then
  echo "refusing deploy: found $running bbgo processes (expected <=1)" >&2
  pgrep -u "$(id -un)" -a -x bbgo || true
  rm -f "$staging"
  exit 1
fi

echo "==> stopping $SERVICE (graceful, keepOrders)"
sudo systemctl stop "$SERVICE"

# Wait until the binary releases the listen port / process exits
deadline=$((SECONDS + STOP_TIMEOUT))
while pgrep -u "$(id -un)" -x bbgo >/dev/null 2>&1; do
  if (( SECONDS >= deadline )); then
    echo "bbgo did not exit within ${STOP_TIMEOUT}s after stop" >&2
    sudo systemctl status "$SERVICE" --no-pager || true
    rm -f "$staging"
    sudo systemctl start "$SERVICE" || true
    exit 1
  fi
  sleep 1
done

if [[ -e "$current" ]]; then
  cp -a "$current" "$backup"
  echo "==> backup $backup"
fi

mv -f "$staging" "$current"
chmod 0755 "$current"
printf '%s\n' "$GIT_SHA" >"$BBGO_HOME/VERSION"
printf '%s\n' "$ts" >"$BBGO_HOME/DEPLOYED_AT"

echo "==> starting $SERVICE"
sudo systemctl start "$SERVICE"

deadline=$((SECONDS + START_TIMEOUT))
ok=0
while (( SECONDS < deadline )); do
  if ! systemctl is-active --quiet "$SERVICE"; then
    sleep 1
    continue
  fi
  # Any HTTP response (incl. 401/404) means the webserver is up.
  code=$(curl -sS -o /dev/null -w '%{http_code}' --connect-timeout 2 "$HEALTH_URL" || true)
  if [[ "$code" =~ ^[12345][0-9][0-9]$ ]]; then
    ok=1
    echo "==> health $HEALTH_URL -> HTTP $code"
    break
  fi
  sleep 1
done

if [[ "$ok" != "1" ]]; then
  echo "health check failed; rolling back to $backup" >&2
  sudo systemctl stop "$SERVICE" || true
  sleep 2
  if [[ -f "$backup" ]]; then
    mv -f "$backup" "$current"
  fi
  sudo systemctl start "$SERVICE" || true
  sudo systemctl status "$SERVICE" --no-pager || true
  exit 1
fi

# Keep a handful of recent backups
ls -1t "$BBGO_HOME"/bbgo.bak.* 2>/dev/null | tail -n +6 | xargs -r rm -f

echo "==> deployed $GIT_SHA successfully"
systemctl is-active "$SERVICE"
