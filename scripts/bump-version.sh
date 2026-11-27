#!/usr/bin/env bash
# Bump all wailsail version placeholders from the value stored in ./version.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION_FILE="version"

usage() {
  cat <<'EOF'
Usage: scripts/bump-version.sh <new-version>

Reads the current version from ./version (e.g. 0.3.0-beta), updates every
version placeholder across frontend/ and build/, and writes the new version
back to ./version.

Examples:
  ./scripts/bump-version.sh 0.3.1-beta
  ./scripts/bump-version.sh v0.4.0-beta
  ./scripts/bump-version.sh 1.0.0
EOF
}

if [[ $# -ne 1 || "$1" == "-h" || "$1" == "--help" ]]; then
  usage
  [[ $# -eq 1 ]] && exit 0 || exit 1
fi

if [[ ! -f "$VERSION_FILE" ]]; then
  echo "Error: $VERSION_FILE not found in $ROOT" >&2
  exit 1
fi

OLD="$(tr -d '[:space:]' < "$VERSION_FILE")"
NEW="${1#v}"

semver_re='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
if [[ ! "$OLD" =~ $semver_re ]]; then
  echo "Error: invalid current version in $VERSION_FILE: '$OLD'" >&2
  exit 1
fi
if [[ ! "$NEW" =~ $semver_re ]]; then
  echo "Error: invalid target version '$1' (expected X.Y.Z or X.Y.Z-prerelease)" >&2
  exit 1
fi
if [[ "$OLD" == "$NEW" ]]; then
  echo "Version is already $NEW; nothing to change."
  exit 0
fi

format_display() {
  local ver="$1"
  local num="${ver%%-*}"
  if [[ "$ver" == *-* ]]; then
    printf 'v%s (%s)' "$num" "${ver#*-}"
  else
    printf 'v%s' "$num"
  fi
}

OLD_NUM="${OLD%%-*}"
NEW_NUM="${NEW%%-*}"
OLD_QUAD="${OLD_NUM}.0"
NEW_QUAD="${NEW_NUM}.0"
OLD_DISPLAY="$(format_display "$OLD")"
NEW_DISPLAY="$(format_display "$NEW")"

replace_literal() {
  local file="$1"
  local from="$2"
  local to="$3"
  if [[ ! -f "$file" ]]; then
    echo "Error: missing file $file" >&2
    exit 1
  fi
  if ! grep -Fq -- "$from" "$file"; then
    echo "Error: expected '$from' in $file (version file may be out of sync)" >&2
    exit 1
  fi
  local tmp="${file}.tmp.$$"
  awk -v from="$from" -v to="$to" '
    {
      out = ""
      s = $0
      while ((idx = index(s, from)) > 0) {
        out = out substr(s, 1, idx - 1) to
        s = substr(s, idx + length(from))
      }
      print out s
    }
  ' "$file" > "$tmp"
  mv "$tmp" "$file"
}

replace_after_key() {
  local file="$1"
  local key="$2"
  local from="$3"
  local to="$4"
  if [[ ! -f "$file" ]]; then
    echo "Error: missing file $file" >&2
    exit 1
  fi
  local tmp="${file}.tmp.$$"
  awk -v key="$key" -v from="$from" -v to="$to" '
    BEGIN { armed = 0; replaced = 0 }
    {
      if (armed) {
        idx = index($0, from)
        if (idx > 0) {
          $0 = substr($0, 1, idx - 1) to substr($0, idx + length(from))
          replaced = 1
        }
        armed = 0
      }
      if (index($0, key) > 0) {
        armed = 1
      }
      print
    }
    END { if (!replaced) exit 2 }
  ' "$file" > "$tmp" || {
    rm -f "$tmp"
    echo "Error: expected '$from' after '$key' in $file (version file may be out of sync)" >&2
    exit 1
  }
  mv "$tmp" "$file"
}

echo "==> Bumping version: $OLD -> $NEW"

replace_literal "frontend/src/lib/store.svelte.ts" "\"$OLD_DISPLAY\"" "\"$NEW_DISPLAY\""
replace_literal "build/config.yml"                 "\"$OLD\""         "\"$NEW\""
replace_literal "build/linux/nfpm/nfpm.yaml"       "\"$OLD\""         "\"$NEW\""
replace_after_key "build/darwin/Info.plist"        "<key>CFBundleShortVersionString</key>" "<string>$OLD</string>"     "<string>$NEW</string>"
replace_after_key "build/darwin/Info.plist"        "<key>CFBundleVersion</key>"            "<string>$OLD_NUM</string>" "<string>$NEW_NUM</string>"
replace_after_key "build/darwin/Info.dev.plist"    "<key>CFBundleShortVersionString</key>" "<string>$OLD</string>"     "<string>$NEW</string>"
replace_after_key "build/darwin/Info.dev.plist"    "<key>CFBundleVersion</key>"            "<string>$OLD_NUM</string>" "<string>$NEW_NUM</string>"
replace_literal "build/windows/info.json"          "\"file_version\": \"$OLD_NUM\""        "\"file_version\": \"$NEW_NUM\""
replace_literal "build/windows/info.json"          "\"ProductVersion\": \"$OLD\""          "\"ProductVersion\": \"$NEW\""
replace_literal "build/windows/wails.exe.manifest" "version=\"$OLD_NUM\""                  "version=\"$NEW_NUM\""
replace_literal "build/windows/nsis/wails_tools.nsh" "\"$OLD_NUM\""                        "\"$NEW_NUM\""
replace_literal "build/windows/msix/app_manifest.xml" "Version=\"$OLD_QUAD\""              "Version=\"$NEW_QUAD\""
replace_literal "build/windows/msix/template.xml"     "Version=\"$OLD_QUAD\""              "Version=\"$NEW_QUAD\""

printf '%s\n' "$NEW" > "$VERSION_FILE"

echo "==> Updated version placeholders and $VERSION_FILE to $NEW"
echo "    Reminder: move [Unreleased] notes in CHANGELOG.md under ## [$NEW] - $(date +%Y-%m-%d)"
