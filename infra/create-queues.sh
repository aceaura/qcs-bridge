#!/usr/bin/env bash
# Creates the three FIFO queues for qcs-bridge. Run once with an identity
# allowed to create queues. Idempotent.
set -euo pipefail

REGION="${AWS_REGION:-us-east-1}"

for name in qcs-commands qcs-results qcs-heartbeat; do
  aws sqs create-queue \
    --region "$REGION" \
    --queue-name "${name}.fifo" \
    --attributes '{
      "FifoQueue": "true",
      "ContentBasedDeduplication": "false",
      "MessageRetentionPeriod": "3600",
      "VisibilityTimeout": "30",
      "SqsManagedSseEnabled": "true"
    }' \
    --output text --query QueueUrl
done
