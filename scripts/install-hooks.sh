#!/bin/sh
# Install git hooks for tdtp-framework
# Run once after cloning: sh scripts/install-hooks.sh

set -e

REPO_ROOT=$(git rev-parse --show-toplevel)
HOOKS_DIR="$REPO_ROOT/.git/hooks"
SCRIPTS_DIR="$REPO_ROOT/scripts/hooks"

echo "Installing git hooks..."

cp "$SCRIPTS_DIR/pre-commit" "$HOOKS_DIR/pre-commit"
chmod +x "$HOOKS_DIR/pre-commit"

echo "✓ pre-commit hook installed"
echo ""
echo "Optional: install gitleaks for stronger secrets detection:"
echo "  https://github.com/gitleaks/gitleaks#installing"
echo "  brew install gitleaks        # macOS"
echo "  winget install gitleaks      # Windows"
echo "  apt install gitleaks         # Ubuntu (via package or binary)"
