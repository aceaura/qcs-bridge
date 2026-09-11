#!/usr/bin/env bash
# Kept for backward compatibility (docs and the OFFLINE hint reference it).
# Equivalent to: qcs-start --background
# Any arguments are forwarded to the agent, e.g. restart-qcs.sh --allow-all
set -euo pipefail

if [[ -x "$HOME/.qcs/qcs-start" ]]; then
  exec "$HOME/.qcs/qcs-start" --background "$@"
fi

# Pre-install fallback (binary still in home dir)
export QCS_CMD_QUEUE="${QCS_CMD_QUEUE:-qcs-commands.fifo}"
export QCS_RESULT_QUEUE="${QCS_RESULT_QUEUE:-qcs-results.fifo}"
export QCS_HEARTBEAT_QUEUE="${QCS_HEARTBEAT_QUEUE:-qcs-heartbeat.fifo}"

pkill -f qcs-agent 2>/dev/null || true
nohup "$HOME/qcs-agent" "$@" >> "$HOME/qcs-agent.log" 2>&1 &
echo "qcs-agent started (pid $!), logs: ~/qcs-agent.log, audit: ~/qcs-audit.log"
echo "tip: run infra/install-agent.sh to install into ~/.qcs and get the qcs-start command"
