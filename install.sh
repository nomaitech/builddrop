#!/usr/bin/env bash
# Builds and installs the build server (systemd user service) and the agent skill.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
GO="$(command -v go || echo "$HOME/.local/share/go/bin/go")"

echo "==> Building server"
mkdir -p "$HOME/.local/bin"
(cd "$ROOT/server" && "$GO" build -o "$HOME/.local/bin/builddrop-server" .)

# Stop the old service before starting BuildDrop on the same port.
if systemctl --user cat android-build-server.service >/dev/null 2>&1; then
  systemctl --user disable --now android-build-server.service
fi

# Preserve existing build history when upgrading from Build Updater.
OLD_DATA="$HOME/.local/share/android-build-server"
NEW_DATA="$HOME/.local/share/builddrop-server"
if [[ -d "$OLD_DATA" && ! -e "$NEW_DATA" ]]; then
  mv "$OLD_DATA" "$NEW_DATA"
fi

echo "==> Installing systemd user service"
mkdir -p "$HOME/.config/systemd/user"
cp "$ROOT/deploy/builddrop-server.service" "$HOME/.config/systemd/user/"
systemctl --user daemon-reload
systemctl --user enable builddrop-server.service
systemctl --user restart builddrop-server.service

echo "==> Installing Codex and Claude Code skills (symlinks)"
CODEX_SKILLS_DIR="${CODEX_HOME:-$HOME/.codex}/skills"
mkdir -p "$CODEX_SKILLS_DIR" "$HOME/.claude/skills"
for SKILLS_DIR in "$CODEX_SKILLS_DIR" "$HOME/.claude/skills"; do
  SKILL_TARGET="$SKILLS_DIR/android-build"
  if [[ -e "$SKILL_TARGET" && ! -L "$SKILL_TARGET" ]]; then
    echo "Keeping existing skill at $SKILL_TARGET; bundled skill: $ROOT/skill/android-build"
  else
    ln -sfn "$ROOT/skill/android-build" "$SKILL_TARGET"
  fi
done

echo "Done. Server: http://$(tailscale ip -4 2>/dev/null | head -1 || echo localhost):8085"
