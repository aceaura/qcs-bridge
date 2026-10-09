#!/usr/bin/env bash
# Runs the qcs-agent container persistently on a fresh EC2 instance
# (Amazon Linux 2023 recommended). The instance role supplies AWS credentials
# through IMDS, so no long-lived keys ever touch disk.
#
# Usage (on the instance, as a sudo-capable user):
#   QCS_SECRET='qcs1:<region>:<secret>' ./ec2-bootstrap.sh
# or create ~/.qcs-secret (mode 600) beforehand and omit the env var.
# Deliver the secret via SSM Session Manager, never via user data.
#
# Prerequisites:
#   - instance role with infra/ec2-instance-policy.json attached
#     (replace REGION/ACCOUNT_ID first)
#   - an EKS access entry (or aws-auth mapping) for that role, or
#     kubectl/eksctl cannot authenticate to the cluster
set -euo pipefail

IMAGE="${IMAGE:-ghcr.io/aceaura/qcs-bridge:latest}"
CONTAINER=qcs-bridge

if ! command -v docker >/dev/null 2>&1; then
  sudo dnf install -y docker
fi
sudo systemctl enable --now docker

SECRET_FILE="$HOME/.qcs-secret"
if [ ! -f "$SECRET_FILE" ]; then
  if [ -z "${QCS_SECRET:-}" ]; then
    echo "no secret: set QCS_SECRET or create $SECRET_FILE first" >&2
    exit 1
  fi
  umask 077
  printf '%s\n' "$QCS_SECRET" > "$SECRET_FILE"
fi

sudo docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
# --network host keeps IMDS reachable at its default hop limit of 1, so the
# AWS SDK inside the container picks up the instance role.
sudo docker run -d --name "$CONTAINER" --restart unless-stopped --network host \
  -v "$SECRET_FILE:/home/qcs/.qcs-secret:ro" \
  -v qcs-logs:/home/qcs/logs \
  "$IMAGE"

echo "qcs-agent started; verify from your workstation with: qcs status"
