#!/usr/bin/env bash
# Installs the full toolchain for restaurant-margin-copilot: Homebrew, Go,
# Node, gh, Docker CLI, uv, and the Spec Kit (SDD) CLI.
#
# Run this yourself, interactively — it needs your sudo password for the
# Homebrew install and opens a browser for the GitHub login:
#   bash docs/install-toolchain.sh

set -euo pipefail

echo "== Homebrew =="
if ! command -v brew >/dev/null 2>&1; then
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  if [[ -d /opt/homebrew/bin ]]; then
    eval "$(/opt/homebrew/bin/brew shellenv)"
    if ! grep -q 'brew shellenv' ~/.zprofile 2>/dev/null; then
      echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile
    fi
  fi
else
  echo "Homebrew already installed, skipping."
fi

echo
echo "== Packages: go, node, gh, docker, docker-compose, uv =="
brew install go node gh docker docker-compose uv

echo
echo "== GitHub CLI auth =="
if gh auth status >/dev/null 2>&1; then
  echo "gh already authenticated, skipping."
else
  gh auth login
fi

echo
echo "== Spec Kit (SDD) CLI =="
uv tool install specify-cli --from git+https://github.com/github/spec-kit.git
specify check || true

echo
echo "== Summary =="
echo "Go:     $(go version 2>/dev/null || echo MISSING)"
echo "Node:   $(node --version 2>/dev/null || echo MISSING)"
echo "gh:     $(gh --version 2>/dev/null | head -1 || echo MISSING)"
echo "Docker: $(docker --version 2>/dev/null || echo MISSING)  (Desktop app must also be installed & running)"
echo "uv:     $(uv --version 2>/dev/null || echo MISSING)"
echo "specify:$(specify --version 2>/dev/null || echo MISSING)"
echo
echo "If Docker shows MISSING or the daemon isn't running, install Docker Desktop"
echo "from https://www.docker.com/products/docker-desktop/ and start it once."
