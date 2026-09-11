# qcs-bridge agent container.
#
# The agent runs remote commands through `bash -c`, so the image has to carry
# the tools its read-only allowlist permits (kubectl, aws, and the basic
# diagnostics) or those commands fail with "command not found" instead of
# running. See internal/proto/proto.go for the allowlist.
#
# No credentials and no secret are baked in — supply both at run time.
# See the run examples at the bottom of this file.

# --- stage 1: build the agent ---------------------------------------------
FROM golang:1.25.5-bookworm AS build
WORKDIR /src

# Warm the module cache separately so code edits don't refetch dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# Static binary: the runtime stage has a different libc surface, and a
# self-contained agent is easier to copy around.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" \
    -o /out/qcs-agent ./cmd/qcs-agent

# --- stage 2: runtime -----------------------------------------------------
FROM debian:bookworm-slim

ARG AWSCLI_VERSION=2.31.11
ARG KUBECTL_VERSION=v1.34.1
ARG TARGETARCH=amd64

# bash: the agent executes commands via `bash -c`.
# ca-certificates: TLS to the SQS and STS endpoints.
# curl, iputils-ping, dnsutils, traceroute: allowlisted diagnostics.
# unzip, less, groff: awscli v2 installer and its paged output.
RUN apt-get update && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        dnsutils \
        groff \
        iputils-ping \
        less \
        traceroute \
        unzip \
    && rm -rf /var/lib/apt/lists/*

# awscli v2 (the bundled installer; there is no apt package for v2).
RUN case "$TARGETARCH" in \
      amd64) awsarch=x86_64 ;; \
      arm64) awsarch=aarch64 ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && curl -fsSL -o /tmp/awscliv2.zip \
        "https://awscli.amazonaws.com/awscli-exe-linux-${awsarch}-${AWSCLI_VERSION}.zip" \
    && unzip -q /tmp/awscliv2.zip -d /tmp \
    && /tmp/aws/install --bin-dir /usr/local/bin --install-dir /usr/local/aws-cli \
    && rm -rf /tmp/awscliv2.zip /tmp/aws

# kubectl.
RUN curl -fsSL -o /usr/local/bin/kubectl \
        "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" \
    && chmod 0755 /usr/local/bin/kubectl

COPY --from=build /out/qcs-agent /usr/local/bin/qcs-agent

# Run unprivileged: the agent only needs outbound HTTPS, and read-only mode is
# a policy check in the agent, not a kernel one — a non-root user limits what a
# bypass could reach.
RUN useradd --create-home --shell /bin/bash --uid 10001 qcs
USER qcs
WORKDIR /home/qcs

# The agent writes ~/qcs-audit.log; keep it outside the container layer so the
# audit trail survives a restart.
VOLUME ["/home/qcs/logs"]

# Fail fast on a missing secret rather than sitting in a crash loop silently.
ENTRYPOINT ["/usr/local/bin/qcs-agent"]

# Read-only allowlist stays on by default. Append --allow-all at `docker run`
# only if you accept arbitrary remote command execution in this container.
CMD []

# --- run examples ---------------------------------------------------------
# The secret carries the region (qcs1:<region>:<secret>), so nothing else is
# needed for region selection.
#
#   docker run --rm \
#     -e QCS_SECRET="qcs1:us-west-1:<secret>" \
#     -v "$HOME/.aws:/home/qcs/.aws:ro" \
#     -e AWS_PROFILE=aws-1 \
#     ghcr.io/aceaura/qcs-bridge:latest
#
# Or with the secret as a mounted file instead of an environment variable
# (keeps it out of `docker inspect`):
#
#   docker run --rm \
#     -v "$HOME/.qcs-secret:/home/qcs/.qcs-secret:ro" \
#     -v "$HOME/.aws:/home/qcs/.aws:ro" \
#     ghcr.io/aceaura/qcs-bridge:latest
