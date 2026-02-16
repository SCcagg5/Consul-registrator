FROM golang:1.22-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY src/go.mod ./
RUN go mod download

COPY src/ ./

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/consul-registrator

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/consul-registrator /consul-registrator

EXPOSE 9090

HEALTHCHECK --interval=10s \
            --timeout=1s \
            --start-interval=500ms \
            --start-period=5s \
            --retries=3 \
           CMD ["/consul-registrator", "--healthcheck"]

ENTRYPOINT ["/consul-registrator"]
