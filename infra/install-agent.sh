#!/usr/bin/env bash
# qcs-bridge agent installer for AWS CloudShell.
# Installs everything into ~/.qcs and wires up the `qcs-start` command.
#
# Network install (recommended, inside CloudShell):
#   curl -fsSL https://raw.githubusercontent.com/aceaura/qcs-bridge/main/infra/install-agent.sh | bash
# Local binary (if you uploaded qcs-agent yourself):
#   bash install-agent.sh [path-to-qcs-agent-binary]
set -euo pipefail

RELEASE_BASE="https://github.com/aceaura/qcs-bridge/releases/download/v0.1.0"
INSTALL_DIR="$HOME/.qcs"
mkdir -p "$INSTALL_DIR"

# --- 1. locate and install the binary (or download it from the release) ---
SRC="${1:-}"
if [[ -z "$SRC" ]]; then
  for cand in "$HOME/qcs-agent" "$HOME/cloudshell_upload/qcs-agent" ./qcs-agent; do
    if [[ -f "$cand" ]]; then SRC="$cand"; break; fi
  done
fi
if [[ -n "$SRC" && -f "$SRC" ]]; then
  cp "$SRC" "$INSTALL_DIR/qcs-agent"
else
  echo "qcs-agent binary not found locally, downloading from GitHub release..."
  curl -fSL -o "$INSTALL_DIR/qcs-agent" "$RELEASE_BASE/qcs-agent"
fi
chmod +x "$INSTALL_DIR/qcs-agent"
echo "[ok] binary installed: $INSTALL_DIR/qcs-agent"

# --- 2. install the launcher ---
cat > "$INSTALL_DIR/qcs-start" <<'LAUNCHER'
#!/usr/bin/env bash
# Starts qcs-agent. Foreground by default (live interaction display, Ctrl+C to stop).
# qcs-start --background  -> run under nohup, follow with: tail -f ~/qcs-agent.log
set -euo pipefail
export QCS_CMD_QUEUE="${QCS_CMD_QUEUE:-qcs-commands.fifo}"
export QCS_RESULT_QUEUE="${QCS_RESULT_QUEUE:-qcs-results.fifo}"
export QCS_HEARTBEAT_QUEUE="${QCS_HEARTBEAT_QUEUE:-qcs-heartbeat.fifo}"

if [[ "${1:-}" == "--background" || "${1:-}" == "-d" ]]; then
  pkill -f '.qcs/qcs-agent' 2>/dev/null || true
  nohup "$HOME/.qcs/qcs-agent" >> "$HOME/qcs-agent.log" 2>&1 &
  echo "qcs-agent started in background (pid $!)"
  echo "watch live: tail -f ~/qcs-agent.log    audit: ~/qcs-audit.log"
else
  echo "qcs-agent starting in foreground — every command and its full output will show below."
  echo "Ctrl+C to stop. (For background mode: qcs-start --background)"
  exec "$HOME/.qcs/qcs-agent"
fi
LAUNCHER
chmod +x "$INSTALL_DIR/qcs-start"
echo "[ok] launcher installed: $INSTALL_DIR/qcs-start"

# --- 3. PATH ---
if ! grep -q '.qcs' "$HOME/.bashrc" 2>/dev/null; then
  echo 'export PATH="$HOME/.qcs:$PATH"' >> "$HOME/.bashrc"
  echo "[ok] added ~/.qcs to PATH in ~/.bashrc"
fi
export PATH="$HOME/.qcs:$PATH"

# --- 4. shared secret ---
if [[ ! -s "$HOME/.qcs-secret" ]]; then
  echo
  echo "Shared HMAC secret not found. Paste the secret (same one as your local machine):"
  read -rs -p "secret: " SECRET
  echo
  if [[ -z "$SECRET" ]]; then
    echo "ERROR: empty secret"; exit 1
  fi
  printf '%s' "$SECRET" > "$HOME/.qcs-secret"
  chmod 600 "$HOME/.qcs-secret"
  echo "[ok] secret saved to ~/.qcs-secret"
else
  echo "[ok] secret already present at ~/.qcs-secret"
fi

echo
echo "Install complete. Start the agent any time with:"
echo
echo "    qcs-start"
echo
echo "(new shells will have it on PATH; in this shell run: export PATH=\"\$HOME/.qcs:\$PATH\")"
