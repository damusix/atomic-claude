# syntax=docker/dockerfile:1

# ── builder ──────────────────────────────────────────────────────────────────
FROM golang:1.23-bookworm AS builder

WORKDIR /src
COPY . /src/

# Derive a version string from the git tree (tags → always → dirty suffix).
# Falls back to "dev" if git history is unavailable (e.g. shallow clone with
# no tags). Commit SHA is captured separately for the binary's --version output.
RUN git describe --tags --always --dirty 2>/dev/null > /tmp/version.txt || echo "dev" > /tmp/version.txt
RUN git rev-parse --short HEAD 2>/dev/null > /tmp/commit.txt || echo "unknown" > /tmp/commit.txt

RUN cd atomic && \
    go generate ./... && \
    CGO_ENABLED=0 go build \
        -ldflags="-s -w \
            -X github.com/damusix/atomic-claude/atomic/internal/version.Version=$(cat /tmp/version.txt) \
            -X github.com/damusix/atomic-claude/atomic/internal/version.Commit=$(cat /tmp/commit.txt)" \
        -o /out/atomic \
        ./cmd/atomic

# ── runtime ──────────────────────────────────────────────────────────────────
FROM node:22-bookworm-slim

# UID of the host user; default 1000. Pass --build-arg HOST_UID=$(id -u) on
# Linux to avoid root-owned files in ./tmp/ bind mounts.
ARG HOST_UID=1000

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git \
        curl \
        ca-certificates \
        less \
        tini && \
    rm -rf /var/lib/apt/lists/*

# Install Claude Code globally.
RUN npm install -g @anthropic-ai/claude-code

# Create a non-root user whose UID matches the host user.
# GID 1000 may already exist in the node base image; use --non-unique to
# tolerate collisions, and fall back gracefully if the group already exists.
RUN groupadd --gid "${HOST_UID}" --non-unique atomic 2>/dev/null || true && \
    useradd --uid "${HOST_UID}" --gid "${HOST_UID}" --non-unique \
            -m -d /home/atomic -s /bin/bash atomic

# Copy the compiled atomic binary and the shared entrypoint.
COPY --from=builder /out/atomic /usr/local/bin/atomic
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# Create the workspace dir and hand it to the atomic user.
RUN mkdir -p /workspace && chown atomic:atomic /workspace

WORKDIR /workspace
USER atomic

ENTRYPOINT ["tini", "--", "/usr/local/bin/docker-entrypoint.sh"]
CMD ["claude"]
