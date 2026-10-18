#!/usr/bin/env bash
# Publish the Homebrew formula for a Neuron release into a tap.
#
# The formula is rendered by .github/workflows/release.yml and attached to every
# release as `neuron.rb`, so this script only has to copy it into the tap repo.
#
#   ./scripts/publish-tap.sh            # latest release
#   ./scripts/publish-tap.sh 0.1.0      # a specific release
#
# Environment:
#   NEURON_REPO  source repository (default: ranscky/neuron)
#   NEURON_TAP   tap repository (default: ranscky/homebrew-neuron)
#
# Requires the GitHub CLI, authenticated (`gh auth login`) with repo scope. This
# is deliberately not part of the release workflow: it creates a repository and
# pushes to it, which should be a human decision the first time.
set -euo pipefail

REPO="${NEURON_REPO:-ranscky/neuron}"
TAP="${NEURON_TAP:-ranscky/homebrew-neuron}"
VERSION="${1:-${VERSION:-}}"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

command -v gh >/dev/null 2>&1 || die "the GitHub CLI is required: https://cli.github.com"

if [ -z "$VERSION" ]; then
  VERSION="$(gh release view --repo "$REPO" --json tagName --jq '.tagName' | sed 's/^v//')" \
    || die "could not determine the latest release"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "Fetching the formula for v${VERSION} from ${REPO}..."
gh release download "v${VERSION}" --repo "$REPO" --pattern neuron.rb --dir "$work" \
  || die "release v${VERSION} does not contain neuron.rb"

if ! gh repo view "$TAP" >/dev/null 2>&1; then
  echo "Creating ${TAP}..."
  gh repo create "$TAP" --public \
    --description "Homebrew formulae for Neuron" >/dev/null
fi

echo "Updating ${TAP}..."
gh repo clone "$TAP" "${work}/tap" -- --depth 1 >/dev/null 2>&1 \
  || die "could not clone ${TAP}"

mkdir -p "${work}/tap/Formula"
cp "${work}/neuron.rb" "${work}/tap/Formula/neuron.rb"

if [ -f "${work}/tap/README.md" ]; then :; else
  cat > "${work}/tap/README.md" <<EOF
# homebrew-neuron

Homebrew tap for the [Neuron](https://github.com/${REPO}) CLI.

\`\`\`bash
brew install ${TAP%%/*}/neuron/neuron
\`\`\`
EOF
fi

cd "${work}/tap"
git add -A
git -c user.name="neuron-release" -c user.email="noreply@github.com" \
  commit -m "neuron ${VERSION}" >/dev/null
git push -u origin HEAD:main

echo ""
echo "Published neuron ${VERSION}."
echo "Install it with: brew install ${TAP%%/*}/neuron/neuron"
