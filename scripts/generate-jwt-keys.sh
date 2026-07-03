#!/bin/bash
# generate-jwt-keys.sh
#
# Generate an RSA key pair for JWT signing.
# The paths must match auth.rsa_private_key / auth.rsa_public_key in config.yaml.
#
# Usage:
#   ./scripts/generate-jwt-keys.sh

set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
KEYS_DIR="${PROJECT_DIR}/configs/keys"
PRIVATE_KEY="${KEYS_DIR}/jwt-private.pem"
PUBLIC_KEY="${KEYS_DIR}/jwt-public.pem"

mkdir -p "$KEYS_DIR"

if [ -f "$PRIVATE_KEY" ] || [ -f "$PUBLIC_KEY" ]; then
    echo "WARNING: JWT keys already exist in ${KEYS_DIR}."
    echo "         Delete them first if you want to regenerate."
    exit 0
fi

echo "==> Generating RSA key pair for JWT ..."
ssh-keygen -t rsa -b 4096 -m PEM -f "$PRIVATE_KEY" -N ""
# Extract the public key in PEM format
ssh-keygen -e -m PEM -f "$PRIVATE_KEY" > "$PUBLIC_KEY"

chmod 600 "$PRIVATE_KEY"
chmod 644 "$PUBLIC_KEY"

echo "==> Done."
echo "    Private key: ${PRIVATE_KEY}"
echo "    Public key:  ${PUBLIC_KEY}"
