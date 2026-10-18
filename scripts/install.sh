#!/usr/bin/env bash
# Install the Neuron CLI.
#
# From a release (no Go toolchain needed):
#   curl -fsSL https://raw.githubusercontent.com/ranscky/neuron/main/scripts/install.sh | sh
#
# From a checkout:
#   ./scripts/install.sh                     # latest release
#   ./scripts/install.sh --version 0.1.0     # a specific release
#   ./scripts/install.sh --source            # build from this checkout with Go
#
# Environment:
#   VERSION      same as --version
#   PREFIX       same as --prefix (default: ~/.local/bin)
#   NEURON_REPO  owner/name to download from (default: ranscky/neuron)
#   GITHUB_TOKEN used only for the release lookup, to dodge API rate limits
set -euo pipefail

REPO="${NEURON_REPO:-ranscky/neuron}"
RELEASE_BASE="${NEURON_RELEASE_BASE:-https://github.com/${REPO}/releases/download}"
API_BASE="${NEURON_API_BASE:-https://api.github.com/repos/${REPO}}"

VERSION="${VERSION:-}"
PREFIX="${PREFIX:-${HOME:-/usr/local}/.local/bin}"
FROM_SOURCE=0
DRY_RUN=0
VERIFY=1

SCRIPT_DIR=""
if [ -n "${BASH_SOURCE:-}" ] && [ -f "${BASH_SOURCE[0]:-}" ]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi

usage() {
  cat <<'EOF'
Install the Neuron CLI.

Usage:
  install.sh [options]

Options:
  --version <v>    Release to install (default: the latest release)
  --prefix <dir>   Where to put the binary (default: ~/.local/bin)
  --source         Build from source instead of downloading (requires Go)
  --no-verify      Skip checksum verification (not recommended)
  --dry-run        Print what would be downloaded and installed, then exit
  -h, --help       Show this help

Environment: VERSION, PREFIX, NEURON_REPO, GITHUB_TOKEN
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

note() {
  printf '%s\n' "$*"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:-}"; [ -n "$VERSION" ] || die "--version needs a value"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --prefix) PREFIX="${2:-}"; [ -n "$PREFIX" ] || die "--prefix needs a value"; shift 2 ;;
    --prefix=*) PREFIX="${1#*=}"; shift ;;
    --source) FROM_SOURCE=1; shift ;;
    --no-verify) VERIFY=0; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; die "unknown argument: $1" ;;
  esac
done

detect_os() {
  case "$(uname -s)" in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    MINGW*|MSYS*|CYGWIN*) echo windows ;;
    *) return 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    arm64|aarch64) echo arm64 ;;
    *) return 1 ;;
  esac
}

latest_version() {
  api="${API_BASE}/releases/latest"
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    json="$(curl -fsSL -H "Authorization: Bearer ${GITHUB_TOKEN}" "$api")" || return 1
  else
    json="$(curl -fsSL "$api")" || return 1
  fi
  tag="$(printf '%s' "$json" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n 1)"
  [ -n "$tag" ] || return 1
  echo "${tag#v}"
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    return 1
  fi
}

extract_archive() {
  archive="$1"
  dest="$2"
  case "$archive" in
    *.zip)
      if command -v unzip >/dev/null 2>&1; then
        unzip -q -o "$archive" -d "$dest"
      else
        # Git Bash ships tar (bsdtar) but not always unzip.
        tar -xf "$archive" -C "$dest"
      fi
      ;;
    *)
      tar -xzf "$archive" -C "$dest"
      ;;
  esac
}

install_binary() {
  src="$1"
  mkdir -p "$PREFIX" || die "could not create $PREFIX"
  cp "$src" "$PREFIX/neuron" || die "could not write to $PREFIX — try --prefix \${HOME}/.local/bin"
  chmod 0755 "$PREFIX/neuron"
}

install_from_source() {
  command -v go >/dev/null 2>&1 || die "--source needs a Go toolchain (https://go.dev/dl)"

  srcdir=""
  if [ -n "$SCRIPT_DIR" ] && [ -d "${SCRIPT_DIR}/../cli" ]; then
    srcdir="$(cd "${SCRIPT_DIR}/.." && pwd)"
  else
    command -v git >/dev/null 2>&1 || die "git is required to fetch the source"
    note "Cloning https://github.com/${REPO} ..."
    git clone --depth 1 "https://github.com/${REPO}" "${tmp}/src"
    srcdir="${tmp}/src"
  fi

  build_version="${VERSION:-0.0.0-dev}"
  note "Building neuron ${build_version} from source..."
  (
    cd "${srcdir}/cli"
    go build -trimpath -ldflags "-s -w -X main.Version=${build_version}" -o "${tmp}/neuron" ./cmd/neuron
  )
  install_binary "${tmp}/neuron"
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

os="$(detect_os)" || die "unsupported operating system: $(uname -s)"
arch="$(detect_arch)" || die "unsupported architecture: $(uname -m)"

if [ "$FROM_SOURCE" = 1 ]; then
  install_from_source
  note ""
  note "Installed $("${PREFIX}/neuron" version) to ${PREFIX}/neuron"
  case ":${PATH}:" in
    *":${PREFIX}:"*) ;;
    *) note "Note: ${PREFIX} is not on your PATH." ;;
  esac
  exit 0
fi

if [ -z "$VERSION" ]; then
  VERSION="$(latest_version)" || die "could not determine the latest release — pass --version, or use --source"
fi

ext="tar.gz"
if [ "$os" = windows ]; then ext="zip"; fi
asset="neuron_${VERSION}_${os}_${arch}.${ext}"
url="${RELEASE_BASE}/v${VERSION}/${asset}"
checksums_url="${RELEASE_BASE}/v${VERSION}/checksums.txt"

if [ "$DRY_RUN" = 1 ]; then
  note "os:        ${os}"
  note "arch:      ${arch}"
  note "version:   ${VERSION}"
  note "asset:     ${url}"
  note "checksums: ${checksums_url}"
  note "prefix:    ${PREFIX}/neuron"
  exit 0
fi

note "Downloading ${url}"
curl -fsSL --retry 3 "$url" -o "${tmp}/${asset}" || die "download failed — does release v${VERSION} publish ${asset}?"

if [ "$VERIFY" = 1 ]; then
  if ! curl -fsSL --retry 3 "$checksums_url" -o "${tmp}/checksums.txt"; then
    die "could not download checksums.txt — refusing to install unverified (use --no-verify to override)"
  fi
  # Checksum lines are "hash  name". Strip the markers a platform may add
  # (sha256sum's binary '*' and a leading "./") so the match cannot silently miss.
  expected="$(awk -v name="$asset" '{ f = $2; sub(/^\*/, "", f); sub(/^\.\//, "", f); if (f == name) print $1 }' "${tmp}/checksums.txt")"
  [ -n "$expected" ] || die "${asset} is not listed in checksums.txt"
  actual="$(sha256_of "${tmp}/${asset}")" || die "no sha256 tool available (need sha256sum or shasum)"
  if [ "$expected" != "$actual" ]; then
    die "checksum mismatch for ${asset}: expected ${expected}, got ${actual}"
  fi
  note "Checksum verified."
fi

mkdir -p "${tmp}/extract"
extract_archive "${tmp}/${asset}" "${tmp}/extract"

bin="neuron"
[ "$os" = windows ] && bin="neuron.exe"
src="$(find "${tmp}/extract" -type f -name "$bin" | head -n 1)"
[ -n "$src" ] || die "the archive did not contain ${bin}"

install_binary "$src"

note ""
note "Installed $("${PREFIX}/neuron" version) to ${PREFIX}/neuron"
case ":${PATH}:" in
  *":${PREFIX}:"*) ;;
  *) note "Note: ${PREFIX} is not on your PATH. Add it: export PATH=\"${PREFIX}:\$PATH\"" ;;
esac
