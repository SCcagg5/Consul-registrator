#!/usr/bin/env bash
set -euo pipefail

ARCH="${ARCH:-$(uname -m)}"
KEEP="${KEEP:-5}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

case "${ARCH}" in
  x86_64|amd64)
    CONSUL_ARCH="amd64"
    ENVOY_ARCH="x86_64"
    ;;
  aarch64|arm64)
    CONSUL_ARCH="arm64"
    ENVOY_ARCH="aarch_64"
    ;;
  *)
    echo "Unsupported architecture: ${ARCH}" >&2
    exit 1
    ;;
esac

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "${TMP_DIR}"; }
trap cleanup EXIT

########################################
# Download Consul (KEEP latest versions)
########################################

CONSUL_INDEX_URL="https://releases.hashicorp.com/consul/index.json"

echo "→ Fetching Consul versions index"
curl -fsSL "${CONSUL_INDEX_URL}" -o "${TMP_DIR}/consul-index.json"

CONSUL_VERSIONS=$(
  jq -r '
    .versions
    | keys[]
    | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))
  ' "${TMP_DIR}/consul-index.json" \
  | sort -Vr \
  | head -n "${KEEP}"
)

echo "→ Selected Consul versions:"
echo "${CONSUL_VERSIONS}" | sed 's/^/  - v/'

for VERSION in ${CONSUL_VERSIONS}; do
  VERSION_TAG="v${VERSION}"
  TGZ="consul-${VERSION_TAG}-linux-${CONSUL_ARCH}.tgz"
  OUTPUT="${SCRIPT_DIR}/${TGZ}"

  if [ -f "${OUTPUT}" ]; then
    echo "↷ ${TGZ} already exists, skipping"
    continue
  fi

  ZIP="consul_${VERSION}_linux_${CONSUL_ARCH}.zip"
  SUMS="consul_${VERSION}_SHA256SUMS"
  BASE_URL="https://releases.hashicorp.com/consul/${VERSION}"

  echo "→ Downloading Consul ${VERSION_TAG}"

  curl -fsSL "${BASE_URL}/${ZIP}"  -o "${TMP_DIR}/${ZIP}"
  curl -fsSL "${BASE_URL}/${SUMS}" -o "${TMP_DIR}/${SUMS}"

  (
    cd "${TMP_DIR}"
    grep "${ZIP}" "${SUMS}" | sha256sum -c -
  )

  unzip -oq "${TMP_DIR}/${ZIP}" consul -d "${TMP_DIR}"
  chmod +x "${TMP_DIR}/consul"

  tar -czf "${OUTPUT}" -C "${TMP_DIR}" consul
  rm -f "${TMP_DIR:?}/"*

  echo "✔ ${TGZ} ready"
done

########################################
# Download Envoy (KEEP latest versions)
########################################

ENVOY_INDEX_URL="https://api.github.com/repos/envoyproxy/envoy/releases"

echo "→ Fetching Envoy releases"
curl -fsSL "${ENVOY_INDEX_URL}" -o "${TMP_DIR}/envoy-releases.json"

ENVOY_RELEASES=$(
  jq -r '
    .[]
    | select(.prerelease == false)
    | {tag: .tag_name, assets: .assets}
  ' "${TMP_DIR}/envoy-releases.json"
)

COUNT=0

echo "→ Resolving Envoy binaries"

echo "${ENVOY_RELEASES}" | jq -c '.' | while read -r release; do
  TAG="$(echo "${release}" | jq -r '.tag')"
  VERSION="${TAG#v}"

  ASSET_URL="$(
    echo "${release}" | jq -r --arg arch "${ENVOY_ARCH}" '
      .assets[]
      | select(
          (.name | test("envoy"))
          and (.name | test("linux"))
          and (.name | test($arch))
          and ((.name | test("\\.sha256$")) | not)
      )
      | .browser_download_url
    ' | head -n1
  )"


  if [ -z "${ASSET_URL}" ]; then
    continue
  fi

  TGZ="envoy-v${VERSION}-linux-${ENVOY_ARCH}.tgz"
  OUTPUT="${SCRIPT_DIR}/${TGZ}"

  if [ -f "${OUTPUT}" ]; then
    echo "↷ ${TGZ} already exists, skipping"
    COUNT=$((COUNT+1))
    [ "${COUNT}" -ge "${KEEP}" ] && break
    continue
  fi

  echo "→ Downloading Envoy ${TAG}"
  curl -fsSL "${ASSET_URL}" -o "${TMP_DIR}/envoy"

  chmod +x "${TMP_DIR}/envoy"
  tar -czf "${OUTPUT}" -C "${TMP_DIR}" envoy

  echo "✔ ${TGZ} ready"

  COUNT=$((COUNT+1))
  [ "${COUNT}" -ge "${KEEP}" ] && break
done

########################################
# Export latest artifacts (for Docker)
########################################

LATEST_CONSUL="$(ls -1 ${SCRIPT_DIR}/consul-v*-linux-${CONSUL_ARCH}.tgz | sort -Vr | head -n1 | xargs -n1 basename)"
LATEST_ENVOY="$(ls -1 ${SCRIPT_DIR}/envoy-v*-linux-${ENVOY_ARCH}.tgz | sort -Vr | head -n1 | xargs -n1 basename)"

cat <<EOF

✔ Done – binaries available in ${SCRIPT_DIR}

Use these build args / env vars:
  CONSUL_TGZ=${LATEST_CONSUL}
  ENVOY_TGZ=${LATEST_ENVOY}

EOF
