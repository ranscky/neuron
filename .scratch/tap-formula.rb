# Homebrew formula for the Neuron CLI.
#
# This is a template. .github/workflows/release.yml fills in the version and the
# sha256 of each archive, attaches the rendered formula to the release as
# `neuron.rb`, and scripts/publish-tap.sh copies it into a tap.
#
# Install straight from a tap once it exists:
#   brew install ranscky/neuron/neuron
class Neuron < Formula
  desc "MCP control plane for developers — one manifest, every AI client"
  homepage "https://github.com/ranscky/neuron"
  version "0.1.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ranscky/neuron/releases/download/v0.1.0/neuron_0.1.0_darwin_arm64.tar.gz"
      sha256 "af11f88e6077af6ba0cd6f7b3259441a1c2cd244c5b0fcbdca0f29c863a3cba5"
    else
      url "https://github.com/ranscky/neuron/releases/download/v0.1.0/neuron_0.1.0_darwin_amd64.tar.gz"
      sha256 "dcb099db5936c0ffe6ef6a028eaa3b99bea0899f46991a31585e2c647bb8de94"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ranscky/neuron/releases/download/v0.1.0/neuron_0.1.0_linux_arm64.tar.gz"
      sha256 "8d1922fe5a53c37252fb948218fee095a32d36ea3b270ec29592de9975a4e6b9"
    else
      url "https://github.com/ranscky/neuron/releases/download/v0.1.0/neuron_0.1.0_linux_amd64.tar.gz"
      sha256 "5d5b9035b022dab562b1576e1f067578790f00860ec9ffcf6efff63233d3a3ec"
    end
  end

  def install
    bin.install "neuron"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/neuron version")
  end
end
