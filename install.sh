#!/usr/bin/env bash
set -euo pipefail

REPO="damusix/atomic-claude"
BINARY="atomic"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"
INSTALL_DIR="${ATOMIC_INSTALL_DIR:-${DEFAULT_INSTALL_DIR}}"

# --------------------------------------------------------------------------- #
# OS detection
# --------------------------------------------------------------------------- #
_os() {
    local raw
    raw="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "${raw}" in
        linux*)   echo "linux" ;;
        darwin*)  echo "darwin" ;;
        mingw*|msys*|cygwin*)
            echo ""
            return 1
            ;;
        *)
            echo "unsupported OS: ${raw}" >&2
            exit 1
            ;;
    esac
}

# --------------------------------------------------------------------------- #
# Arch detection
# --------------------------------------------------------------------------- #
_arch() {
    local raw
    raw="$(uname -m)"
    case "${raw}" in
        x86_64|amd64)    echo "amd64" ;;
        aarch64|arm64)   echo "arm64" ;;
        *)
            echo "unsupported architecture: ${raw}" >&2
            echo "Please download manually from https://github.com/${REPO}/releases" >&2
            exit 1
            ;;
    esac
}

# --------------------------------------------------------------------------- #
# Semver comparison: returns 0 if $1 >= $2 (both in X.Y.Z[-pre] form).
# Handles pre-release per semver 2.0.0: "1.0.0-rc1" < "1.0.0".
# If the patch field contains non-numeric chars (pre-release suffix), the
# version is treated as less than any release of the same X.Y.Z, so we
# fall through to force-install rather than blocking it.
# --------------------------------------------------------------------------- #
_semver_gte() {
    local a="${1}" b="${2}"
    local a_major a_minor a_patch b_major b_minor b_patch
    IFS='.' read -r a_major a_minor a_patch <<< "${a}"
    IFS='.' read -r b_major b_minor b_patch <<< "${b}"

    # Strip only the leading numeric part; keep non-numeric chars as a signal.
    local a_patch_num b_patch_num
    a_patch_num="${a_patch%%[^0-9]*}"
    b_patch_num="${b_patch%%[^0-9]*}"

    # If either patch field has a non-numeric suffix (e.g. "-rc1"), it's a
    # pre-release and must be treated as less than the bare release.
    local a_pre=0 b_pre=0
    [ "${a_patch}" != "${a_patch_num}" ] && a_pre=1
    [ "${b_patch}" != "${b_patch_num}" ] && b_pre=1

    # Strip non-numeric from major/minor (should not have pre-release there).
    a_major="${a_major//[^0-9]/}"
    a_minor="${a_minor//[^0-9]/}"
    b_major="${b_major//[^0-9]/}"
    b_minor="${b_minor//[^0-9]/}"

    [ "${a_major:-0}" -gt "${b_major:-0}" ] && return 0
    [ "${a_major:-0}" -lt "${b_major:-0}" ] && return 1
    [ "${a_minor:-0}" -gt "${b_minor:-0}" ] && return 0
    [ "${a_minor:-0}" -lt "${b_minor:-0}" ] && return 1
    [ "${a_patch_num:-0}" -gt "${b_patch_num:-0}" ] && return 0
    [ "${a_patch_num:-0}" -lt "${b_patch_num:-0}" ] && return 1

    # Same X.Y.Z numeric part. Pre-release < release per semver 2.0.0.
    # If a is pre-release and b is not: a < b → return 1
    if [ "${a_pre}" -eq 1 ] && [ "${b_pre}" -eq 0 ]; then
        return 1
    fi
    # If a is release and b is pre-release: a >= b → return 0
    if [ "${a_pre}" -eq 0 ] && [ "${b_pre}" -eq 1 ]; then
        return 0
    fi
    # Both pre-release or both release: treat as equal (conservative).
    return 0
}

# --------------------------------------------------------------------------- #
# Main
# --------------------------------------------------------------------------- #

OS="$(_os || true)"
if [ -z "${OS}" ]; then
    echo "Windows is not supported by this installer." >&2
    echo "Please download the binary from https://github.com/${REPO}/releases" >&2
    echo "and place it in a directory on your PATH." >&2
    exit 1
fi

ARCH="$(_arch)"

# Resolve version
if [ -n "${ATOMIC_VERSION:-}" ]; then
    TAG="${ATOMIC_VERSION}"
else
    TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
         | grep '"tag_name"' \
         | head -1 \
         | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')"
fi

if [ -z "${TAG}" ]; then
    echo "error: could not determine latest release tag." >&2
    exit 1
fi

# Version without leading 'v'
VERSION="${TAG#v}"

# --------------------------------------------------------------------------- #
# Refusal: already up-to-date
# --------------------------------------------------------------------------- #
if [ -x "${INSTALL_DIR}/${BINARY}" ]; then
    INSTALLED_OUTPUT="$("${INSTALL_DIR}/${BINARY}" --version 2>&1 || true)"
    # Extract X.Y.Z from output
    INSTALLED_VER="$(printf '%s' "${INSTALLED_OUTPUT}" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
    if [ -n "${INSTALLED_VER}" ] && [ -n "${VERSION}" ]; then
        if _semver_gte "${INSTALLED_VER}" "${VERSION}" 2>/dev/null; then
            echo "atomic ${INSTALLED_VER} is already up to date (${TAG})."
            exit 0
        fi
    else
        echo "warning: could not parse existing version; proceeding with install." >&2
    fi
fi

ARCHIVE_NAME="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
ARCHIVE_URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE_NAME}"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"

# --------------------------------------------------------------------------- #
# Temp dir + cleanup
# --------------------------------------------------------------------------- #
TMP_DIR="${TMPDIR:-/tmp}/atomic-install-$$"
mkdir -p "${TMP_DIR}"
trap 'rm -rf "${TMP_DIR}"' EXIT

# --------------------------------------------------------------------------- #
# Download
# --------------------------------------------------------------------------- #
echo "Downloading ${ARCHIVE_NAME} ..."
curl -fsSL -o "${TMP_DIR}/${ARCHIVE_NAME}" "${ARCHIVE_URL}"
curl -fsSL -o "${TMP_DIR}/checksums.txt" "${CHECKSUMS_URL}"

# --------------------------------------------------------------------------- #
# Verify SHA256
# --------------------------------------------------------------------------- #
echo "Verifying checksum ..."
if command -v shasum >/dev/null 2>&1; then
    ACTUAL="$(shasum -a 256 "${TMP_DIR}/${ARCHIVE_NAME}" | awk '{print $1}')"
else
    ACTUAL="$(sha256sum "${TMP_DIR}/${ARCHIVE_NAME}" | awk '{print $1}')"
fi

EXPECTED="$(grep "  ${ARCHIVE_NAME}$" "${TMP_DIR}/checksums.txt" | awk '{print $1}')"

if [ -z "${EXPECTED}" ]; then
    echo "error: ${ARCHIVE_NAME} not found in checksums.txt" >&2
    exit 1
fi

if [ "${ACTUAL}" != "${EXPECTED}" ]; then
    echo "error: checksum mismatch" >&2
    echo "  expected: ${EXPECTED}" >&2
    echo "  actual:   ${ACTUAL}" >&2
    exit 1
fi

# --------------------------------------------------------------------------- #
# Extract + install
# --------------------------------------------------------------------------- #
STAGING="${TMP_DIR}/staging"
mkdir -p "${STAGING}"
tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "${STAGING}"

mkdir -p "${INSTALL_DIR}"
install -m 0755 "${STAGING}/${BINARY}" "${INSTALL_DIR}/${BINARY}"

# --------------------------------------------------------------------------- #
# Success message
# --------------------------------------------------------------------------- #
echo ""
echo "atomic ${TAG} installed at ${INSTALL_DIR}/${BINARY}."
echo ""
echo "To install the atomic-claude artifact bundle (CLAUDE.md, agents, commands,"
echo "skills, output-styles) into ~/.claude/, run:"
echo ""
echo "    atomic claude install"
echo ""
echo "To install only signals / reminders helpers without touching ~/.claude/,"
echo "skip the above."

# PATH reminder
case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
        echo ""
        echo "note: ${INSTALL_DIR} is not on your PATH. add 'export PATH=\"${INSTALL_DIR}:\$PATH\"' to your shell rc."
        ;;
esac
