#!/bin/sh
set -e

: "${CONSUL_SERVER:?CONSUL_SERVER must be set to true or false}"
: "${CONSUL_NODE_NAME:?CONSUL_NODE_NAME must be set}"
: "${CONSUL_GOSSIP_KEY:?CONSUL_GOSSIP_KEY must be set}"

CONSUL_ARGS=""

if [ "$CONSUL_SERVER" = "true" ]; then
  : "${CONSUL_ALLOW_BOOTSTRAP:?CONSUL_ALLOW_BOOTSTRAP must be set when CONSUL_SERVER=true}"

  CONSUL_ARGS="$CONSUL_ARGS -server"

  RAFT_DIR="/consul/data/raft"

  if [ "$CONSUL_ALLOW_BOOTSTRAP" = "true" ]; then
    if [ -d "$RAFT_DIR" ]; then
      echo "ℹ️ Raft data already exists → disabling bootstrap"
      export CONSUL_BOOTSTRAP_EXPECT=0
    else
      echo "🚀 Bootstrap allowed and no raft data → bootstrap_expect=1"
      export CONSUL_BOOTSTRAP_EXPECT=1
    fi
  else
    echo "ℹ️ Bootstrap not allowed → bootstrap_expect=0"
    export CONSUL_BOOTSTRAP_EXPECT=0
  fi
  CONSUL_ARGS="$CONSUL_ARGS -bootstrap-expect=$CONSUL_BOOTSTRAP_EXPECT"
fi

if [ -n "${CONSUL_PEERS:-}" ]; then
  OLD_IFS="$IFS"
  IFS=','

  for peer in $CONSUL_PEERS; do
    if [ -n "$peer" ]; then
      CONSUL_ARGS="$CONSUL_ARGS -retry-join=$peer"
    fi
  done

  IFS="$OLD_IFS"
fi



export CONSUL_SERVER_BOOL CONSUL_BOOTSTRAP_EXPECT_INT

TLS_DIR="/consul/tls"
mkdir -p "$TLS_DIR/ca" "$TLS_DIR/id"

CA_CERT="$TLS_DIR/ca/ca.pem"
CA_KEY="$TLS_DIR/ca/ca.key"
SERIAL_FILE="$TLS_DIR/ca.srl"
CERT="$TLS_DIR/id/consul.pem"
KEY="$TLS_DIR/id/consul-key.pem"

[ -f "$CA_CERT" ] || { echo "❌ Missing CA certificate"; exit 1; }
[ -f "$CA_KEY" ]  || { echo "❌ Missing CA private key"; exit 1; }

if [ ! -f "$CERT" ] || [ ! -f "$KEY" ]; then
  echo "🔐 Generating Consul agent TLS certificate"

  openssl genrsa -out "$KEY" 2048

  openssl req -new \
    -key "$KEY" \
    -subj "/CN=consul-agent" \
    -out /tmp/consul.csr

  cat > /tmp/consul-ext.cnf <<EOF
subjectAltName = DNS:consul,IP:127.0.0.1
extendedKeyUsage = serverAuth,clientAuth
EOF
  [ -f "$SERIAL_FILE" ] || echo 01 > "$SERIAL_FILE"

  openssl x509 -req \
    -in /tmp/consul.csr \
    -CA "$CA_CERT" \
    -CAkey "$CA_KEY" \
    -CAserial "$SERIAL_FILE" \
    -out "$CERT" \
    -days 825 \
    -sha256 \
    -extfile /tmp/consul-ext.cnf

  rm -f /tmp/consul.csr /tmp/consul-ext.cnf
fi

printf '%s' "$CONSUL_GOSSIP_KEY" | od -An -tx1

exec /bin/consul agent \
  -client=0.0.0.0 \
  -node="$CONSUL_NODE_NAME" \
  -encrypt="$CONSUL_GOSSIP_KEY" \
  -data-dir=/consul/data \
  -config-dir=/consul/config \
  $CONSUL_ARGS
