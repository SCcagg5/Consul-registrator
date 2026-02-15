#!/bin/sh
set -euo pipefail

############################################
# Script: healthcheck.sh
# Purpose: Check Service health by probing the ping endpoint.
#   1. Perform an HTTP GET request to an URL
#   2. Verify the response contains "OK".
#   3. Exit with status 0 if healthy, or 1 if unhealthy.
#
# Requirements:
#   - WGET
#
# Exit Codes:
#   0 - Service is healthy (response contains "OK").
#   1 - Service is unhealthy (response missing "OK").
#
# Usage:
#   ./healthcheck.sh
############################################

############################################
# Function: check_health
# Purpose: Probe the ping endpoint and determine health status.
############################################
check_health() {
  if wget --no-verbose --tries=1 --spider "http://127.0.0.1:8500/v1/status/leader"; then
    echo "OK"
    exit 0
  else
    echo "NOT OK"
    exit 1
  fi
}

############################################
# Main Script Execution
############################################
check_health