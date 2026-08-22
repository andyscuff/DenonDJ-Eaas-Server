# mattn/go-sqlite3 (used for the optional Navidrome integration) is cgo, so
# the build stage needs a C toolchain — golang:bookworm has gcc/libc-dev
# already. The runtime stage stays glibc-based (debian-slim) to match the
# cgo-linked binary; a musl/Alpine base would need CGO cross-compiled instead.
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/storage ./cmd/storage
RUN CGO_ENABLED=1 go build -trimpath -o /out/storage ./cmd/storage

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/storage /usr/local/bin/storage

# 11224/udp: EAAS device discovery (broadcast — needs --network host, a
# bridged/NATed container is invisible to the beacon regardless of this
# EXPOSE). 50010/tcp: EAAS gRPC. 50020/tcp: artwork + file HTTP.
EXPOSE 11224/udp 50010/tcp 50020/tcp

ENTRYPOINT ["/usr/local/bin/storage"]
CMD ["--music-dir", "/music"]
