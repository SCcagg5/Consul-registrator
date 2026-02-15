#!/bin/sh
set -eu

############################################
# Script: consul/certs/generate.sh
# Purpose: Generate or refresh the Consul CA certificate.
# Notes:
#   - POSIX-compliant.
#   - Idempotent.
#   - CA private key is NEVER regenerated.
#   - --force only regenerates ca.pem, never ca.key.
#
# Arguments:
#   --host    Required (kept for interface consistency).
#   --force   Optional. Regenerate ca.pem using existing ca.key.
############################################

HOST=""
FORCE=0

fail() {
  echo "Error: $*" >&2
  exit 1
}

############################################
# Function: resolve_script_dir
# Purpose: Resolve the absolute directory of this script, following a single symlink.
############################################
resolve_script_dir() {
  src="$0"
  if [ -L "$src" ]; then
    # Follow the symlink target (single level, sufficient for common setups)
    src="$(readlink "$src")"
  fi
  dir="$(dirname "$src")"
  (CDPATH= cd "$dir" && pwd)
}

############################################
# Parse arguments
############################################
while [ $# -gt 0 ]; do
  case "$1" in
    --host)
      HOST="$2"
      shift 2
      ;;
    --force)
      FORCE=1
      shift
      ;;
    *)
      fail "Unknown arg: $1"
      ;;
  esac
done

[ -n "$HOST" ] || fail "--host is required"


CERTS_DIR="$(resolve_script_dir)"
CA_KEY="$CERTS_DIR/ca.key"
CA_CERT="$CERTS_DIR/ca.pem"

############################################
# Function: issue_ca_cert
# Purpose: Create ca.pem from existing ca.key.
############################################
issue_ca_cert() {
  openssl req -x509 -new -nodes \
    -key "$CA_KEY" \
    -sha256 \
    -days 3650 \
    -subj "/CN=Consul CA" \
    -out "$CA_CERT"
}

############################################
# CA logic
############################################

# Invalid state: cert exists without key
if [ -f "$CA_CERT" ] && [ ! -f "$CA_KEY" ]; then
  fail "ca.pem exists but ca.key is missing (refusing to continue)"
fi

# Initial generation (neither exists)
if [ ! -f "$CA_KEY" ] && [ ! -f "$CA_CERT" ]; then
  echo "[certs] Generating new Consul CA key and certificate"

  openssl genrsa -out "$CA_KEY" 4096
  issue_ca_cert

# Regenerate certificate only (force or missing cert)
elif [ "$FORCE" -eq 1 ] || [ ! -f "$CA_CERT" ]; then
  echo "[certs] Regenerating Consul CA certificate (key preserved)"

  issue_ca_cert

else
  echo "[certs] Consul CA already present, nothing to do"
fi

echo "[certs] Consul CA ready in $CERTS_DIR"
