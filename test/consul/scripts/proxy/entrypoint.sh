#!/bin/sh
set -e

# CA_CERT="/consul/tls/ca/ca.pem"

exec /bin/consul connect envoy \
  -sidecar-for "${SERVICE_NAME}" \
  -admin-bind 127.0.0.1:19000 \
  -envoy-ready-bind-address 127.0.0.1 \
  -envoy-ready-bind-port 19100 \
  -grpc-addr "${CONSUL_GRPC_ADDR}" \
  -http-addr "${CONSUL_HTTP_ADDR}"
  # -grpc-ca-file "${CA_CERT}" \
  
