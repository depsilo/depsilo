# typed: false
# frozen_string_literal: true

# Placeholder generated-formula shape. The Homebrew tap is not published yet
# and the checksums below are intentionally invalid. Once tap publishing is
# enabled, GoReleaser will generate a formula in the separate tap repository;
# this main-repository placeholder is not updated automatically. Do not install it.

class Depsilo < Formula
  desc "Supply-chain enforcement proxy for 14 ecosystems and Docker OCI"
  homepage "https://depsilo.com"
  version "0.5.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.intel?
      url "https://github.com/depsilo/depsilo/releases/download/v0.5.0/depsilo_0.5.0_darwin_amd64.tar.gz"
      sha256 "REPLACE_WITH_ACTUAL_SHA256"
    end
    if Hardware::CPU.arm?
      url "https://github.com/depsilo/depsilo/releases/download/v0.5.0/depsilo_0.5.0_darwin_arm64.tar.gz"
      sha256 "REPLACE_WITH_ACTUAL_SHA256"
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      url "https://github.com/depsilo/depsilo/releases/download/v0.5.0/depsilo_0.5.0_linux_amd64.tar.gz"
      sha256 "REPLACE_WITH_ACTUAL_SHA256"
    end
    if Hardware::CPU.arm?
      url "https://github.com/depsilo/depsilo/releases/download/v0.5.0/depsilo_0.5.0_linux_arm64.tar.gz"
      sha256 "REPLACE_WITH_ACTUAL_SHA256"
    end
  end

  def install
    bin.install "depsilo"
  end

  test do
    system "#{bin}/depsilo", "version"
  end
end
