FROM alpine:3.21 AS consul_base

ARG CONSUL_TGZ=consul-v1.22.2-linux-amd64.tgz

COPY ./binaries/lib/glibc-openssl-runtime-amd64.tgz /tmp/runtime.tgz
COPY ./binaries/${CONSUL_TGZ} /tmp/consul.tgz

ENV LD_LIBRARY_PATH=/usr/glibc-compat/lib:/usr/lib/x86_64-linux-gnu

RUN set -eux \
    && tar -xzf /tmp/runtime.tgz -C / \
    && rm -f /tmp/runtime.tgz \
    && tar -xzf /tmp/consul.tgz -C /bin \
    && chmod 0755 /bin/consul \
    && rm -f /tmp/consul.tgz \
    && mkdir -p /consul/data /consul/config

FROM consul_base AS consul_node

COPY ./config/node/*.hcl /consul/config/

COPY ./scripts/node/entrypoint.sh /bin/entrypoint.sh
COPY ./scripts/node/healthcheck.sh /bin/healthcheck.sh

RUN chmod 0755 /bin/entrypoint.sh /bin/healthcheck.sh

EXPOSE 8300 \
       8301 8301/udp \
       8302 8302/udp \
       8500 \
       8600 8600/udp

VOLUME ["/consul/data"]

ENTRYPOINT ["/bin/sh", "/bin/entrypoint.sh"]

HEALTHCHECK --interval=10s\
            --timeout=1s \
            --start-interval=500ms \
            --start-period=5s \
            --retries=3 \
            CMD ["/bin/sh", "/bin/healthcheck.sh"]

FROM consul_base AS consul_proxy


COPY ./binaries/lib/iptables-runtime-amd64.tgz /tmp/iptables.tgz
RUN set -eux \
    && tar -xzf /tmp/iptables.tgz -C / \
    && rm -f /tmp/iptables.tgz

ARG ENVOY_TGZ=envoy-v1.34.12-linux-x86_64.tgz
COPY ./binaries/${ENVOY_TGZ} /tmp/envoy.tgz

RUN set -eux \
    && tar -xzf /tmp/envoy.tgz -C /bin \
    && chmod 0755 /bin/envoy \
    && rm -f /tmp/envoy.tgz

COPY ./scripts/proxy/entrypoint.sh /bin/entrypoint.sh
COPY ./scripts/proxy/healthcheck.sh /bin/healthcheck.sh

RUN chmod 0755 /bin/entrypoint.sh /bin/healthcheck.sh

EXPOSE 8443

ENTRYPOINT ["/bin/sh", "/bin/entrypoint.sh"]

HEALTHCHECK --interval=10s\
            --timeout=1s \
            --start-interval=500ms \
            --start-period=5s \
            --retries=3 \
            CMD ["/bin/sh", "/bin/healthcheck.sh"]