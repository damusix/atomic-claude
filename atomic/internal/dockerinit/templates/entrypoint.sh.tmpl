#!/usr/bin/env bash
set -euo pipefail

# Lay/refresh the atomic bundle in ~/.claude (idempotent — SHA-based).
atomic claude install

# Wire the session-start hook (idempotent).
atomic hooks install

# If no credentials file is present, tell the user to log in.
CREDENTIALS_FILE="${HOME}/.claude/.credentials.json"
if [ ! -f "${CREDENTIALS_FILE}" ]; then
    echo ""
    echo "  No Claude credentials found."
    echo "  Run: claude login"
    echo ""
fi

exec "$@"
