#!/usr/bin/env bash
# Install a NEW self-hosted GitHub Actions runner for b1u3h4t/bbgo.
# Safe on nc3 (does NOT touch avalanchego-tuned /home/github-runner/actions-runner).
# Also used on nc2 (Harbor host) for parallel Docker / Go / Deploy capacity.
#
# Usage (as root):
#   export RUNNER_TOKEN=XXXX   # from bbgo → Settings → Actions → Runners → New runner
#   # optional:
#   #   RUNNER_NAME=nc2-bbgo LABELS=self-hosted,Linux,X64,bbgo
#   bash scripts/install-bbgo-runner.sh
#
# Get token (on your laptop after `gh auth login` as b1u3h4t):
#   gh api -X POST repos/b1u3h4t/bbgo/actions/runners/registration-token --jq .token
set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/b1u3h4t/bbgo}"
RUNNER_USER="${RUNNER_USER:-github-runner}"
if ! id "$RUNNER_USER" >/dev/null 2>&1; then
  useradd -m -s /bin/bash "$RUNNER_USER"
fi
# Docker jobs need the socket
if getent group docker >/dev/null 2>&1; then
  usermod -aG docker "$RUNNER_USER" || true
fi

RUNNER_HOME="$(getent passwd "$RUNNER_USER" | cut -d: -f6)"
RUNNER_DIR="${RUNNER_DIR:-$RUNNER_HOME/actions-runner-bbgo}"
RUNNER_NAME="${RUNNER_NAME:-$(hostname)-bbgo}"
LABELS="${LABELS:-self-hosted,Linux,X64,bbgo}"
# Keep in sync with GitHub's current runner release if needed
RUNNER_VERSION="${RUNNER_VERSION:-2.337.0}"
TARBALL="actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"
DOWNLOAD_URL="https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/${TARBALL}"

# Safety: never operate on the avalanchego-tuned install (nc3 only)
AVALANCHE_DIR="$RUNNER_HOME/actions-runner"
if [[ "$(realpath -m "$RUNNER_DIR")" == "$(realpath -m "$AVALANCHE_DIR")" ]]; then
  echo "ERROR: refusing to use avalanchego-tuned dir: $AVALANCHE_DIR" >&2
  exit 1
fi

if [[ -z "${RUNNER_TOKEN:-}" ]]; then
  echo "ERROR: export RUNNER_TOKEN=... (registration token for $REPO_URL)" >&2
  exit 1
fi

mkdir -p "$RUNNER_DIR"
chown "$RUNNER_USER:$RUNNER_USER" "$RUNNER_DIR"
cd "$RUNNER_DIR"

if [[ ! -f ./config.sh ]]; then
  echo "Downloading $TARBALL ..."
  # Prefer local copy from avalanche runner dir to save bandwidth (read-only)
  if [[ -f "$AVALANCHE_DIR/$TARBALL" ]]; then
    sudo -u "$RUNNER_USER" cp "$AVALANCHE_DIR/$TARBALL" .
  else
    sudo -u "$RUNNER_USER" curl -fsSL -o "$TARBALL" "$DOWNLOAD_URL"
  fi
  sudo -u "$RUNNER_USER" tar xzf "$TARBALL"
fi

# If already configured for bbgo, just (re)start
if [[ -f .runner ]]; then
  url=$(python3 -c 'import json;print(json.load(open(".runner"))["gitHubUrl"])' 2>/dev/null || true)
  if [[ "$url" == "$REPO_URL" ]]; then
    echo "Already registered to $url — reinstalling service"
    ./svc.sh stop || true
    ./svc.sh uninstall || true
    ./svc.sh install "$RUNNER_USER"
    ./svc.sh start
    ./svc.sh status || true
    exit 0
  fi
  echo "ERROR: $RUNNER_DIR already configured for: $url" >&2
  echo "Remove it manually if you intend to replace." >&2
  exit 1
fi

sudo -u "$RUNNER_USER" ./config.sh \
  --unattended \
  --url "$REPO_URL" \
  --token "$RUNNER_TOKEN" \
  --name "$RUNNER_NAME" \
  --labels "$LABELS" \
  --work _work

./svc.sh install "$RUNNER_USER"
./svc.sh start
./svc.sh status || systemctl status "actions.runner.*bbgo*" --no-pager | head -40

echo
echo "OK: new runner at $RUNNER_DIR"
echo "  name:   $RUNNER_NAME"
echo "  repo:   $REPO_URL"
echo "  labels: $LABELS"
if [[ -d "$AVALANCHE_DIR" ]]; then
  echo "avalanchego-tuned runner left untouched at $AVALANCHE_DIR"
fi
