# Build the web interface, then the static binary, then copy both into an image
# that holds nothing else: no shell, no package manager.

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web/dist ./web/dist
# CGO is off because the SQLite driver is pure Go, so the binary needs no libc.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/netscribe ./cmd/netscribe
# The data directory is created here because the final image has no shell. A named
# volume copies its owner from this directory on first use.
RUN mkdir /out/data

# The variant with nmap, for discovery.nmap. Build it with --target nmap, or with
# deploy/compose.nmap.yaml. It is not the default: the default image is the last stage.
# nmap gets file capabilities so that the unprivileged user can open raw sockets.
FROM alpine:3.22 AS nmap
RUN apk add --no-cache nmap libcap-utils \
 && setcap cap_net_raw,cap_net_admin,cap_net_bind_service+eip /usr/bin/nmap \
 && addgroup -g 65532 nonroot && adduser -D -u 65532 -G nonroot -h /data nonroot
COPY --from=build /out/netscribe /usr/local/bin/netscribe
COPY --from=build --chown=nonroot:nonroot /out/data /data
WORKDIR /data
USER nonroot:nonroot
ENTRYPOINT ["netscribe"]
CMD ["serve"]

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/netscribe /usr/local/bin/netscribe
COPY --from=build --chown=nonroot:nonroot /out/data /data
# The relative database path in the config resolves to /data/netscribe.db, and the
# default config path resolves to /data/netscribe.yaml, so "netscribe user add" works
# inside the container without flags.
WORKDIR /data
USER nonroot:nonroot
ENTRYPOINT ["netscribe"]
CMD ["serve"]
