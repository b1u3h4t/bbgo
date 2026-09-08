#!/usr/bin/env bash
# Build linux/amd64 bbgo and deploy to the production host via SSH.
#
# Typical usage from CI (self-hosted runner with Host bbgo in ~/.ssh/config):
#   scripts/deploy-prod.sh
#
# Env:
#   BBGO_SSH_HOST   SSH host alias (default: bbgo)
#   BBGO_HOME       remote app dir (default: /home/admin/bbgo)
#   SKIP_BUILD=1    reuse ./dist/bbgo-linux-amd64
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

SSH_HOST=${BBGO_SSH_HOST:-bbgo}
BBGO_HOME=${BBGO_HOME:-/home/admin/bbgo}
GIT_SHA=${GITHUB_SHA:-$(git rev-parse --short HEAD)}
DIST_DIR=${DIST_DIR:-$ROOT/dist}
OUT=$DIST_DIR/bbgo-linux-amd64
REMOTE_TMP=/tmp/bbgo-ci-$GIT_SHA

mkdir -p "$DIST_DIR"

if [[ "${SKIP_BUILD:-}" != "1" ]]; then
  echo "==> building linux/amd64 (tags: release)"
  # release-only matches the trading binary path used in Docker; SPA assets are optional.
  CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -tags release \
    -ldflags "-s -w" \
    -o "$OUT" ./cmd/bbgo
fi

if [[ ! -f "$OUT" ]]; then
  echo "missing binary: $OUT" >&2
  exit 1
fi

chmod +x "$OUT"
echo "==> binary $(du -h "$OUT" | awk '{print $1}') sha256=$(sha256sum "$OUT" | awk '{print $1}')"

echo "==> upload to $SSH_HOST:$REMOTE_TMP"
scp -o BatchMode=yes -o IdentitiesOnly=yes "$OUT" "$SSH_HOST:$REMOTE_TMP"
scp -o BatchMode=yes -o IdentitiesOnly=yes "$ROOT/scripts/deploy-prod-remote.sh" "$SSH_HOST:/tmp/deploy-prod-remote.sh"

echo "==> remote swap"
ssh -o BatchMode=yes -o IdentitiesOnly=yes "$SSH_HOST" \
  "BBGO_HOME='$BBGO_HOME' bash /tmp/deploy-prod-remote.sh '$REMOTE_TMP' '$GIT_SHA' && rm -f '$REMOTE_TMP' /tmp/deploy-prod-remote.sh"

echo "==> done"
