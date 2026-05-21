# Homebrew formula for ArcanFlows CLI
# Install: brew install arcanflows/tap/arcanflows
# Update: brew upgrade arcanflows

class Arcanflows < Formula
  desc "ArcanFlows CLI - AI Automation from your terminal"
  homepage "https://arcanflows.com"
  version "0.7.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/windconsul/ai-saas-agents/releases/download/v#{version}/arcanflows-darwin-arm64"
      sha256 "PLACEHOLDER_SHA256_DARWIN_ARM64"
    else
      url "https://github.com/windconsul/ai-saas-agents/releases/download/v#{version}/arcanflows-darwin-amd64"
      sha256 "PLACEHOLDER_SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/windconsul/ai-saas-agents/releases/download/v#{version}/arcanflows-linux-arm64"
      sha256 "PLACEHOLDER_SHA256_LINUX_ARM64"
    else
      url "https://github.com/windconsul/ai-saas-agents/releases/download/v#{version}/arcanflows-linux-amd64"
      sha256 "PLACEHOLDER_SHA256_LINUX_AMD64"
    end
  end

  def install
    binary_name = "arcanflows"
    if OS.mac?
      if Hardware::CPU.arm?
        bin.install "arcanflows-darwin-arm64" => binary_name
      else
        bin.install "arcanflows-darwin-amd64" => binary_name
      end
    else
      if Hardware::CPU.arm?
        bin.install "arcanflows-linux-arm64" => binary_name
      else
        bin.install "arcanflows-linux-amd64" => binary_name
      end
    end

    # Generate shell completions
    generate_completions_from_executable(bin/"arcanflows", "completion")
  end

  test do
    assert_match "ArcanFlows CLI", shell_output("#{bin}/arcanflows version")
  end
end
