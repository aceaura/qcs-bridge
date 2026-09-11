#!/usr/bin/env bash
# Removes what install-agent.sh created: the ~/.qcs directory, the symlinks it
# placed on PATH, and the PATH line it appended to shell rc files.
#
# Network run:
#   curl -fsSL https://raw.githubusercontent.com/aceaura/qcs-bridge/main/infra/uninstall-agent.sh | bash
#
# The shared secret (~/.qcs-secret) is kept by default so a reinstall stays
# painless. Pass --purge to delete it too.
set -euo pipefail

INSTALL_DIR="$HOME/.qcs"
PURGE=0
[[ "${1:-}" == "--purge" ]] && PURGE=1

# --- 1. stop a running agent ---
if pkill -f '.qcs/qcs-agent' 2>/dev/null; then
  echo "[ok] stopped running qcs-agent"
fi

# --- 2. remove symlinks that point into ~/.qcs ---
for d in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
  for name in qcs-start qcs-agent; do
    link="$d/$name"
    [[ -e "$link" || -L "$link" ]] || continue
    if [[ -L "$link" && "$(readlink "$link")" == "$INSTALL_DIR/"* ]]; then
      rm -f "$link"
      echo "[ok] removed symlink $link"
    elif [[ -f "$link" && -f "$INSTALL_DIR/$name" ]] && cmp -s "$link" "$INSTALL_DIR/$name"; then
      # Some filesystems make `ln -s` copy the file instead of linking.
      rm -f "$link"
      echo "[ok] removed $link"
    fi
  done
done

# --- 3. drop the PATH line install-agent.sh appended ---
for rc in "$HOME/.bashrc" "$HOME/.profile"; do
  [[ -f "$rc" ]] || continue
  # Only our own lines: the tagged one, plus the legacy ~/.qcs export from
  # older installers. An untagged ~/.local/bin export may be the user's.
  if grep -qE '(# qcs-bridge$)|(export PATH="\$HOME/\.qcs:\$PATH")' "$rc"; then
    tmp="$(mktemp)"
    grep -vE '(# qcs-bridge$)|(export PATH="\$HOME/\.qcs:\$PATH")' "$rc" > "$tmp"
    cat "$tmp" > "$rc"
    rm -f "$tmp"
    echo "[ok] removed qcs-bridge PATH line from $rc"
  fi
done

# --- 4. remove the install directory ---
if [[ -d "$INSTALL_DIR" ]]; then
  rm -rf "$INSTALL_DIR"
  echo "[ok] removed $INSTALL_DIR"
fi

# --- 5. secret ---
if [[ $PURGE -eq 1 ]]; then
  rm -f "$HOME/.qcs-secret"
  echo "[ok] removed ~/.qcs-secret"
elif [[ -e "$HOME/.qcs-secret" ]]; then
  echo "[keep] ~/.qcs-secret left in place (re-run with --purge to delete it)"
fi

echo
echo "Uninstall complete. Logs, if any, are still at ~/qcs-agent.log and ~/qcs-audit.log."
