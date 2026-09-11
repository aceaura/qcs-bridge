#!/usr/bin/env bash
# qcs-bridge agent installer for AWS CloudShell.
# Installs everything into ~/.qcs and wires up the `qcs-start` command.
#
# Network install (recommended, inside CloudShell):
#   curl -fsSL https://raw.githubusercontent.com/aceaura/qcs-bridge/main/infra/install-agent.sh | bash
# Local binary (if you uploaded qcs-agent yourself):
#   bash install-agent.sh [path-to-qcs-agent-binary]
set -euo pipefail

RELEASE_BASE="https://github.com/aceaura/qcs-bridge/releases/download/v0.2.0"
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
# Starts qcs-agent (config: ~/.qcs/config, secret: ~/.qcs-secret).
# Foreground by default (live interaction display, Ctrl+C to stop).
# qcs-start --background  -> run under nohup, follow with: tail -f ~/qcs-agent.log
set -euo pipefail

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

# --- 2b. config file (no env vars needed) ---
if [[ ! -s "$INSTALL_DIR/config" ]]; then
  REGION="$(aws configure get region 2>/dev/null || true)"
  {
    echo "# qcs-bridge agent config (env vars with the same names override these)"
    echo "QCS_CMD_QUEUE=${QCS_CMD_QUEUE:-qcs-commands.fifo}"
    echo "QCS_RESULT_QUEUE=${QCS_RESULT_QUEUE:-qcs-results.fifo}"
    echo "QCS_HEARTBEAT_QUEUE=${QCS_HEARTBEAT_QUEUE:-qcs-heartbeat.fifo}"
    [[ -n "$REGION" ]] && echo "AWS_REGION=$REGION"
  } > "$INSTALL_DIR/config"
  chmod 600 "$INSTALL_DIR/config"
  echo "[ok] config written: $INSTALL_DIR/config"
else
  echo "[ok] config already present at $INSTALL_DIR/config"
fi

# --- 3. make qcs-start reachable from the shell that ran this script ---
# A PATH export here would die with this subshell, so link the launcher into a
# directory that is already on the caller's PATH.
on_path() {
  case ":$PATH:" in *":$1:"*) return 0 ;; *) return 1 ;; esac
}

LINK_DIR=""
for d in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
  if on_path "$d" && [[ -d "$d" && -w "$d" ]]; then LINK_DIR="$d"; break; fi
done

if [[ -n "$LINK_DIR" ]]; then
  ln -sf "$INSTALL_DIR/qcs-start" "$LINK_DIR/qcs-start"
  ln -sf "$INSTALL_DIR/qcs-agent" "$LINK_DIR/qcs-agent"
  echo "[ok] linked qcs-start into $LINK_DIR (already on PATH)"
else
  mkdir -p "$HOME/.local/bin"
  ln -sf "$INSTALL_DIR/qcs-start" "$HOME/.local/bin/qcs-start"
  ln -sf "$INSTALL_DIR/qcs-agent" "$HOME/.local/bin/qcs-agent"
  for rc in "$HOME/.bashrc" "$HOME/.profile"; do
    if [[ -f "$rc" ]] && ! grep -q '.local/bin' "$rc" 2>/dev/null; then
      echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$rc"
    fi
  done
  echo "[ok] linked qcs-start into ~/.local/bin and added it to PATH in ~/.bashrc / ~/.profile"
  NEEDS_PATH_EXPORT=1
fi

# --- 4. shared secret ---
if [[ ! -s "$HOME/.qcs-secret" ]]; then
  SECRET="${QCS_SECRET:-}"
  if [[ -z "$SECRET" ]]; then
    # Under `curl ... | bash` stdin is the pipe, so read must come from the terminal.
    if [[ -r /dev/tty ]]; then
      echo
      echo "Shared HMAC secret not found. Paste the secret (same one as your local machine):"
      read -rs -p "secret: " SECRET < /dev/tty
      echo
    fi
  fi
  if [[ -z "$SECRET" ]]; then
    echo
    echo "ERROR: no secret provided. Either re-run with the secret pre-set:"
    echo
    echo "    curl -fsSL https://raw.githubusercontent.com/aceaura/qcs-bridge/main/infra/install-agent.sh | QCS_SECRET=<secret> bash"
    echo
    echo "or write it manually and start the agent:"
    echo
    echo "    printf '%s' '<secret>' > ~/.qcs-secret && chmod 600 ~/.qcs-secret && qcs-start"
    echo
    exit 1
  fi
  printf '%s' "$SECRET" > "$HOME/.qcs-secret"
  chmod 600 "$HOME/.qcs-secret"
  echo "[ok] secret saved to ~/.qcs-secret"
else
  echo "[ok] secret already present at ~/.qcs-secret"
fi

echo
echo "Install complete. Start the agent with:"
echo
if [[ "${NEEDS_PATH_EXPORT:-}" == "1" ]]; then
  echo "    export PATH=\"\$HOME/.local/bin:\$PATH\" && qcs-start"
  echo
  echo "(new shells pick this up automatically; the export is only needed in this one)"
else
  echo "    qcs-start"
fi
