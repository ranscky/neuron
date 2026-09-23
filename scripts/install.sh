#!/usr/bin/env bash
# Build and install the Neuron CLI from source.
#
# Usage:
#   ./scripts/install.sh              # installs to ~/.local/bin
#   PREFIX=/usr/local/bin ./scripts/install.sh
#   VERSION=0.1.0 ./scripts/install.sh
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREFIX="${PREFIX:-$HOME/.local/bin}"
VERSION="${VERSION:-0.1.0-dev}"

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is required to build Neuron — see https://go.dev/dl" >&2
  exit 1
fi

mkdir -p "$PREFIX"

echo "Building neuron ${VERSION}..."
(
  cd "$REPO_DIR/cli"
  go build -ldflags "-s -w -X main.Version=${VERSION}" -o "$PREFIX/neuron" ./cmd/neuron
)

echo "Installed $("$PREFIX/neuron" version) to $PREFIX/neuron"

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) echo "Note: $PREFIX is not on your PATH." ;;
esac
