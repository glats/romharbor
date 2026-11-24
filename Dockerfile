FROM golang:1.25.8-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/web ./cmd/web
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM debian:bookworm-slim

LABEL org.opencontainers.image.source="https://github.com/glats/romharbor" \
      org.opencontainers.image.licenses="MIT"

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
RUN mkdir -p /data/roms /data/romcache && chown 10001:10001 /data /data/roms /data/romcache
COPY --from=build /out/web /app/web
COPY --from=build /out/worker /app/worker
USER 10001:10001
ENV HTTP_ADDR=:8080 DATABASE_PATH=/data/app.db LOG_PRETTY=false \
    ROM_LIBRARY_PATH=/data/roms ROM_CACHE_PATH=/data/romcache ROM_SOURCES=archive.org
EXPOSE 8080
ENTRYPOINT ["/app/web"]
