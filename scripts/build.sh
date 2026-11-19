#!/usr/bin/env bash
# Validate, test, build wailsail, stage OS/arch-named artifacts, and generate SHA256SUMS.
# Works locally and in GitHub Actions (single-job or 3-job test -> build matrix -> release).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MODE="all" # all | test-only | build-only | checksums-only
ARCH="${ARCH:-}"
VERSION="${VERSION:-}"
DIST_DIR="bin/dist"
TARGETS=()

usage() {
  cat <<'EOF'
Usage: scripts/build.sh [options]

Modes (default: validate, test, build, stage, and generate SHA256SUMS):
  --test-only           Run frontend and Go validation/tests only (CI Job 1)
  --skip-tests          Build, stage, and checksum without re-running tests (CI Job 2 matrix)
  --checksums-only      Generate & verify SHA256SUMS for all files in --dist-dir (CI Job 3 release)

Options:
  --target <os>         Target OS: linux, windows, darwin (repeatable or comma-separated; default: host OS)
  --arch <arch>         Target GOARCH: amd64, arm64, ... (default: host GOARCH)
  --version <ver>       Optional version tag in artifact names (e.g. v0.1.3 -> wailsail-v0.1.3-linux-amd64)
  --dist-dir <dir>      Output directory for staged artifacts & SHA256SUMS (default: bin/dist)
  -h, --help            Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --test-only)      MODE="test-only"; shift ;;
    --skip-tests)     MODE="build-only"; shift ;;
    --checksums-only) MODE="checksums-only"; shift ;;
    --target)
      IFS=',' read -r -a parts <<< "${2:?--target requires a value}"
      TARGETS+=("${parts[@]}")
      shift 2
      ;;
    --arch)     ARCH="${2:?--arch requires a value}"; shift 2 ;;
    --version)  VERSION="${2:?--version requires a value}"; shift 2 ;;
    --dist-dir) DIST_DIR="${2:?--dist-dir requires a value}"; shift 2 ;;
    -h|--help)  usage; exit 0 ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

log() { printf '\n==> %s\n' "$*"; }

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command '$1' not found in PATH" >&2
    exit 1
  fi
}

write_and_verify_checksums() {
  local dir="$1"
  shift
  local files=("$@")
  if [[ ${#files[@]} -eq 0 ]]; then
    echo "error: no artifacts found in '$dir' to hash" >&2
    exit 1
  fi

  # Sort deterministically regardless of runner locale.
  local sorted=()
  while IFS= read -r f; do
    sorted+=("$f")
  done < <(printf '%s\n' "${files[@]}" | LC_ALL=C sort)

  log "Generate and verify $dir/SHA256SUMS"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$dir" && sha256sum "${sorted[@]}" > SHA256SUMS && sha256sum -c SHA256SUMS)
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$dir" && shasum -a 256 "${sorted[@]}" > SHA256SUMS && shasum -a 256 -c SHA256SUMS)
  else
    echo "error: neither sha256sum nor shasum found in PATH" >&2
    exit 1
  fi
  cat "$dir/SHA256SUMS"
}

if [[ "$MODE" == "checksums-only" ]]; then
  if [[ ! -d "$DIST_DIR" ]]; then
    echo "error: dist directory '$DIST_DIR' does not exist" >&2
    exit 1
  fi
  FOUND=()
  for p in "$DIST_DIR"/*; do
    [[ -f "$p" ]] || continue
    base="$(basename "$p")"
    [[ "$base" == "SHA256SUMS" ]] && continue
    FOUND+=("$base")
  done
  write_and_verify_checksums "$DIST_DIR" "${FOUND[@]}"
  exit 0
fi

# 1. Validate & test (skipped only when --skip-tests is explicitly passed)
if [[ "$MODE" == "all" || "$MODE" == "test-only" ]]; then
  require_cmd node
  require_cmd npm
  require_cmd go

  log "Frontend: install / check / test / build"
  (
    cd frontend
    if [[ "${CI:-}" == "true" && -f package-lock.json ]]; then
      npm ci
    elif [[ ! -d node_modules ]]; then
      npm install
    fi
    npm run check
    npm test
    npm run build
  )

  log "Go: vet / build / test"
  go vet . ./internal/...
  go build . ./internal/...

  GO_TEST_ARGS=(-count=1)
  HOST_OS="$(go env GOOS)"
  if [[ "${CGO_ENABLED:-1}" != "0" && ( "$HOST_OS" == "linux" || "$HOST_OS" == "darwin" ) ]] && (command -v gcc >/dev/null 2>&1 || command -v clang >/dev/null 2>&1); then
    GO_TEST_ARGS+=(-race)
  fi
  go test "${GO_TEST_ARGS[@]}" . ./internal/...

  if [[ "$MODE" == "test-only" ]]; then
    log "Validation and tests passed (--test-only)"
    exit 0
  fi
fi

# 2. Build & stage OS/arch-named binaries (only after tests pass in default mode)
require_cmd go
require_cmd wails3

if [[ ${#TARGETS[@]} -eq 0 ]]; then
  TARGETS=("$(go env GOOS)")
fi
if [[ -z "$ARCH" ]]; then
  ARCH="$(go env GOARCH)"
fi

mkdir -p "$DIST_DIR"
STAGED=()
for target in "${TARGETS[@]}"; do
  case "$target" in
    linux|darwin) raw="wailsail"; ext="" ;;
    windows)      raw="wailsail.exe"; ext=".exe" ;;
    *)
      echo "error: unsupported target '$target' (expected linux, windows, or darwin)" >&2
      exit 2
      ;;
  esac

  log "Build desktop binary (GOOS=$target ARCH=$ARCH)"
  GOOS="$target" wails3 build "ARCH=$ARCH"

  if [[ ! -f "bin/$raw" ]]; then
    echo "error: expected build output 'bin/$raw' was not produced" >&2
    exit 1
  fi

  name="wailsail${VERSION:+-$VERSION}-${target}-${ARCH}${ext}"
  cp -f "bin/$raw" "$DIST_DIR/$name"
  STAGED+=("$name")
done

# 3. Generate & verify SHA256SUMS over staged release artifacts
write_and_verify_checksums "$DIST_DIR" "${STAGED[@]}"
log "Done. Artifacts and SHA256SUMS are in $DIST_DIR"
