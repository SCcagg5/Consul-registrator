FROM golang:1.22-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    go build -ldflags="-s -w" -o /out/consul-registrator

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/consul-registrator /consul-registrator

EXPOSE 9090

HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD ["/consul-registrator", "--healthcheck"]

ENTRYPOINT ["/consul-registrator"]
