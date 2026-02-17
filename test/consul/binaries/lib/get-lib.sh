docker run --rm -v "$PWD:/out" debian:bookworm-slim bash -lc '
set -e

apt-get update
apt-get install -y --no-install-recommends \
  libc6 \
  libstdc++6 \
  openssl \
  ca-certificates

ROOTFS="/tmp/rootfs"

mkdir -p \
  "$ROOTFS/usr/glibc-compat/lib" \
  "$ROOTFS/lib64" \
  "$ROOTFS/usr/bin" \
  "$ROOTFS/usr/lib/x86_64-linux-gnu" \
  "$ROOTFS/etc/ssl" \
  "$ROOTFS/usr/share" \
  "$ROOTFS/usr/lib/ssl"

cp -av /lib/x86_64-linux-gnu/ld-linux-x86-64.so.2 "$ROOTFS/lib64/"
cp -av /lib/x86_64-linux-gnu/libc.so.6            "$ROOTFS/usr/glibc-compat/lib/"
cp -av /lib/x86_64-linux-gnu/libm.so.6            "$ROOTFS/usr/glibc-compat/lib/"
cp -av /lib/x86_64-linux-gnu/libpthread.so.0      "$ROOTFS/usr/glibc-compat/lib/"
cp -av /lib/x86_64-linux-gnu/librt.so.1           "$ROOTFS/usr/glibc-compat/lib/"
cp -av /lib/x86_64-linux-gnu/libdl.so.2           "$ROOTFS/usr/glibc-compat/lib/"
cp -av /lib/x86_64-linux-gnu/libgcc_s.so.1        "$ROOTFS/usr/glibc-compat/lib/"
cp -av /usr/lib/x86_64-linux-gnu/libstdc++.so.*   "$ROOTFS/usr/glibc-compat/lib/"
cp -av /usr/bin/openssl                           "$ROOTFS/usr/bin/"
cp -av /usr/lib/x86_64-linux-gnu/libssl.so.*      "$ROOTFS/usr/lib/x86_64-linux-gnu/"
cp -av /usr/lib/x86_64-linux-gnu/libcrypto.so.*   "$ROOTFS/usr/lib/x86_64-linux-gnu/"
cp -av /usr/lib/ssl/openssl.cnf                   "$ROOTFS/usr/lib/ssl/"
cp -av /etc/ssl                                   "$ROOTFS/etc/"
cp -av /usr/share/ca-certificates                 "$ROOTFS/usr/share/"
tar -C "$ROOTFS" -czf /out/glibc-openssl-runtime-amd64.tgz .
'

docker run --rm --platform=linux/amd64 -v "$PWD:/out" alpine:3.21 sh -euxc '
ROOT=/tmp/iptables-root
rm -rf "$ROOT"
mkdir -p "$ROOT"

apk add --no-cache --root "$ROOT" --initdb --no-scripts \
  --keys-dir /etc/apk/keys --repositories-file /etc/apk/repositories \
  iptables iptables-legacy

mkdir -p "$ROOT/bin"
if [ -e "$ROOT/usr/sbin/iptables-nft" ]; then
  ln -sf /usr/sbin/iptables-nft "$ROOT/bin/iptables"
else
  ln -sf /usr/sbin/iptables "$ROOT/bin/iptables"
fi

[ -e "$ROOT/usr/sbin/iptables-legacy" ] && ln -sf /usr/sbin/iptables-legacy "$ROOT/bin/iptables-legacy" || true

ls -l "$ROOT/usr/lib/libxtables.so.12"* "$ROOT/usr/lib/libmnl.so.0"* "$ROOT/usr/lib/libnftnl.so.11"* >/dev/null
test -d "$ROOT/usr/lib/xtables"

rm -rf "$ROOT/var/cache/apk" "$ROOT/lib/apk" "$ROOT/usr/share/apk" "$ROOT/etc/apk"

tar -C "$ROOT" -czf /out/iptables-runtime-amd64.tgz .
ls -lh /out/iptables-runtime-amd64.tgz
'
