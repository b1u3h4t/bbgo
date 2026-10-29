#!/usr/bin/env bash
#
# Cross-compile the darwin (macOS) release artifacts on Linux.
#
# `make dist` used to run on a macOS runner because the darwin binaries need
# CGO for github.com/mattn/go-sqlite3, and there is no cross-compiler for that
# on a stock Linux box. zig ships a clang that can emit Mach-O, so with a macOS
# SDK for the framework stubs the same eight binaries can be produced here.
#
# This script only covers darwin. The linux artifacts stay with the Makefile
# (`make dist-bbgo-linux`) and `make dist`'s `desktop` target — which builds a
# macOS .app that the release never uploads — is deliberately not called.
#
# Environment:
#   VERSION         required, e.g. v1.65.0 (drives the artifact names)
#   MACOS_SDK       macOS SDK root          (default /opt/sdk26/MacOSX26.1.sdk)
#   MACOS_MIN_ARM64 deployment target       (default 13.0)
#   MACOS_MIN_AMD64 deployment target       (default 13.0)
#
# The deployment target has to be given explicitly: zig's own default for
# `-target <arch>-macos` is the *newest* OS it knows about (15.0 as of zig
# 0.17), so without a version suffix every artifact would refuse to launch on
# anything older. 13.0 matches what the Go toolchain itself stamps on darwin
# binaries (see macOS in cmd/link/internal/ld/macho.go), so this keeps parity
# with the internal linker's advice rather than inventing a new floor.
#
# Signing: the Makefile can gpg-sign the tarballs when SIGN=1. The release
# workflow does not do that, so this script does not either.

set -euo pipefail

: "${VERSION:?VERSION must be set, e.g. VERSION=v1.65.0}"

SDK="${MACOS_SDK:-/opt/sdk26/MacOSX26.1.sdk}"
MIN_ARM64="${MACOS_MIN_ARM64:-13.0}"
MIN_AMD64="${MACOS_MIN_AMD64:-13.0}"

BIN_DIR="build/bbgo"
DIST_DIR="dist/${VERSION}"

VARIANTS=(
  "bbgo-darwin-arm64"
  "bbgo-darwin-amd64"
  "bbgo-slim-darwin-arm64"
  "bbgo-slim-darwin-amd64"
  "bbgo-dnum-darwin-arm64"
  "bbgo-dnum-darwin-amd64"
  "bbgo-slim-dnum-darwin-arm64"
  "bbgo-slim-dnum-darwin-amd64"
)

die() { echo "::error::$*" >&2; exit 1; }
note() { echo "--- $*"; }

#
# Prerequisites
#
command -v zig >/dev/null 2>&1 || die "zig not found in PATH"
[ -d "$SDK/System/Library/Frameworks" ] \
  || die "macOS SDK not found at $SDK (download one, or point MACOS_SDK at it)"

# The Go toolchain runs `dsymutil` and `strip` after linking a darwin binary.
# GNU strip cannot read Mach-O ("file format not recognized"), so an LLVM one
# has to win on PATH. This is handled here rather than by the caller so the
# script works on a machine where only `apt install llvm` was run.
for tool in dsymutil llvm-strip; do
  command -v "$tool" >/dev/null 2>&1 \
    || die "$tool not found; install llvm (e.g. apt-get install llvm)"
done
if ! strip --version 2>/dev/null | grep -qi llvm; then
  shim_dir=$(mktemp -d)
  ln -sf "$(command -v llvm-strip)" "$shim_dir/strip"
  PATH="$shim_dir:$PATH"
  note "strip is not LLVM's; using $(command -v strip) via $shim_dir"
fi
export PATH

# pkg/server/assets.go embeds the built frontend, and the `web` tag variants
# below depend on it. Building it here would drag in yarn and next.js, so fail
# loudly instead of doing that silently.
[ -f pkg/server/assets.go ] \
  || die "pkg/server/assets.go is missing; run 'make static' first"

mkdir -p "$BIN_DIR" "$DIST_DIR"

#
# Compile. The Makefile targets are reused verbatim so the build tags stay in
# sync with upstream; only the C toolchain is swapped for the duration of each
# architecture, since arm64 and amd64 need different `-target` triples.
#
build_arch() {
  local arch="$1" triple="$2"
  note "darwin/${arch}  (zig cc -target ${triple})"
  env \
    CGO_ENABLED=1 \
    CC="zig cc -target ${triple}" \
    CGO_CFLAGS="-I${SDK}/usr/include -F${SDK}/System/Library/Frameworks -Wno-nullability-completeness" \
    CGO_LDFLAGS="-F${SDK}/System/Library/Frameworks -L${SDK}/usr/lib" \
    make --no-print-directory \
      "bbgo-darwin-${arch}" \
      "bbgo-slim-darwin-${arch}" \
      "bbgo-dnum-darwin-${arch}" \
      "bbgo-slim-dnum-darwin-${arch}"
}

build_arch arm64 "aarch64-macos.${MIN_ARM64}"
build_arch amd64 "x86_64-macos.${MIN_AMD64}"

#
# Verify, then package. The tarball names have to match what `make dist` used
# to write, because the release step globs dist/<version>/*.tar.gz and the
# asset names are user-facing.
#
echo
printf '%-34s %-9s %-8s %-8s %s\n' ARTIFACT ARCH MINOS SIZE STATUS
failures=0

for bin in "${VARIANTS[@]}"; do
  path="${BIN_DIR}/${bin}"
  [ -f "$path" ] || die "missing binary: $path"

  expect_arch=arm64
  expect_min="$MIN_ARM64"
  case "$bin" in
    *-amd64) expect_arch=x86_64; expect_min="$MIN_AMD64" ;;
  esac

  # `file` is in binutils and always present; llvm-readobj only when llvm is.
  file_out=$(file -b "$path")
  got_arch="?"
  case "$file_out" in
    *"Mach-O 64-bit arm64"*) got_arch=arm64 ;;
    *"Mach-O 64-bit x86_64"*) got_arch=x86_64 ;;
  esac

  got_min="-"
  if command -v llvm-readobj >/dev/null 2>&1; then
    got_min=$(llvm-readobj --macho-version-min "$path" 2>/dev/null \
      | awk '/^ *Version:/ {print $2; exit}')
    got_min="${got_min:--}"
  fi

  status=ok
  [ "$got_arch" = "$expect_arch" ] || status="BAD ARCH(${got_arch})"
  if [ "$got_min" != "-" ] && [ "$got_min" != "$expect_min" ]; then
    status="BAD MINOS(${got_min})"
  fi
  [ "$status" = ok ] || failures=$((failures + 1))

  tarball="${bin/-darwin-/-${VERSION}-darwin-}.tar.gz"
  tar -C "$BIN_DIR" -czf "${DIST_DIR}/${tarball}" "$bin"

  printf '%-34s %-9s %-8s %-8s %s\n' \
    "$bin" "$got_arch" "$got_min" "$(du -h "$path" | cut -f1)" "$status"
done

[ "$failures" -eq 0 ] || die "${failures} darwin artifact(s) failed verification"

echo
note "packaged into ${DIST_DIR}:"
ls -1 "${DIST_DIR}"/*.tar.gz | sed 's|^|    |'
echo
echo "darwin cross-compile OK (${#VARIANTS[@]} artifacts)"
