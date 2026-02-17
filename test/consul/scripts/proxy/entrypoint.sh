#!/bin/sh
set -e

if [ "${DUMMY_MODE}" = "true" ]; then
  echo "DUMMY_MODE enabled → exiting successfully"
  exit 0
fi

PROXY_UID=1337
PROXY_USER=envoy
PROXY_ID="${SERVICE_NAME}-sidecar-proxy"

if ! id "${PROXY_USER}" >/dev/null 2>&1; then
  echo "Creating user ${PROXY_USER} (uid ${PROXY_UID})"
  adduser -D -u ${PROXY_UID} ${PROXY_USER}
fi

consul connect redirect-traffic \
  -proxy-id "${PROXY_ID}" \
  -proxy-uid ${PROXY_UID} \
  -exclude-inbound-port 19100 \
  -exclude-inbound-port 20200

GRPC_CA_FLAG=""
if [ -n "${CONSUL_GRPC_CA_FILE}" ] && [ -f "${CONSUL_GRPC_CA_FILE}" ]; then
  echo "Using gRPC CA file: ${CONSUL_GRPC_CA_FILE}"
  GRPC_CA_FLAG="-grpc-ca-file ${CONSUL_GRPC_CA_FILE}"
fi

exec su ${PROXY_USER} -s /bin/sh -c "
/bin/consul connect envoy \
  -sidecar-for \"${SERVICE_NAME}\" \
  -admin-bind 127.0.0.1:19000 \
  -envoy-ready-bind-address 0.0.0.0 \
  -envoy-ready-bind-port 19100 \
  -grpc-addr \"${CONSUL_GRPC_ADDR}\" \
  -http-addr \"${CONSUL_HTTP_ADDR}\" \
  ${GRPC_CA_FLAG}
"
