#!/usr/bin/env bash
# Runs inside CloudShell. Starts (or restarts) qcs-agent in the background.
# CloudShell recycles the VM after ~20-30 min idle, which kills the agent;
# rerun this after reopening CloudShell.
set -euo pipefail

export QCS_CMD_QUEUE="${QCS_CMD_QUEUE:-qcs-commands.fifo}"
export QCS_RESULT_QUEUE="${QCS_RESULT_QUEUE:-qcs-results.fifo}"
export QCS_HEARTBEAT_QUEUE="${QCS_HEARTBEAT_QUEUE:-qcs-heartbeat.fifo}"

pkill -f qcs-agent 2>/dev/null || true
nohup "$HOME/qcs-agent" >> "$HOME/qcs-agent.log" 2>&1 &
echo "qcs-agent started (pid $!), logs: ~/qcs-agent.log, audit: ~/qcs-audit.log"
