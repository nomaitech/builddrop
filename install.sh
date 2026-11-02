#!/usr/bin/env bash
# Builds and installs the build server (systemd user service) and the Claude skill.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
GO="$(command -v go || echo "$HOME/.local/share/go/bin/go")"

echo "==> Building server"
mkdir -p "$HOME/.local/bin"
(cd "$ROOT/server" && "$GO" build -o "$HOME/.local/bin/android-build-server" .)

echo "==> Installing systemd user service"
mkdir -p "$HOME/.config/systemd/user"
cp "$ROOT/deploy/android-build-server.service" "$HOME/.config/systemd/user/"
systemctl --user daemon-reload
systemctl --user enable android-build-server.service
systemctl --user restart android-build-server.service

echo "==> Installing Claude skill (symlink)"
mkdir -p "$HOME/.claude/skills"
ln -sfn "$ROOT/skill/android-build" "$HOME/.claude/skills/android-build"

echo "Done. Server: http://$(tailscale ip -4 2>/dev/null | head -1 || echo localhost):8085"
