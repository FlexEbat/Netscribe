#!/bin/sh
# Runs the Playwright tests against a fresh server: a temporary config and database, the
# demo network and the account "dev" from cmd/seed. The scan schedule is off and the
# discovery sources are off, so nothing here touches a network. Needs ./netscribe (make build).
set -eu

cd "$(dirname "$0")/.."
[ -x ./netscribe ] || { echo "e2e: build the binary first (make build)" >&2; exit 1; }

dir="$(mktemp -d)"
pid=""
cleanup() {
	[ -n "$pid" ] && kill "$pid" 2>/dev/null || true
	rm -rf "$dir"
}
trap cleanup EXIT INT TERM

port="${E2E_PORT:-18080}"
NETSCRIBE_DEV_PASSWORD="${NETSCRIBE_DEV_PASSWORD:-e2e-dev-password-12}"
export NETSCRIBE_DEV_PASSWORD

cat >"$dir/netscribe.yaml" <<YAML
listen: 127.0.0.1:$port
database: $dir/netscribe.db
scan:
  targets: [192.168.1.0/24]
  interval: 0
docker:
  enabled: false
discovery:
  routerTables: false
  httpHeaders: false
  mdns: false
  ssdp: false
  ipv6: false
  tlsCerts: false
updates:
  check: false
YAML
chmod 600 "$dir/netscribe.yaml"

go run ./cmd/seed --config "$dir/netscribe.yaml" --dev-user
./netscribe serve --config "$dir/netscribe.yaml" >"$dir/server.log" 2>&1 &
pid=$!

i=0
until curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 100 ] || ! kill -0 "$pid" 2>/dev/null; then
		echo "e2e: the server did not start" >&2
		cat "$dir/server.log" >&2
		exit 1
	fi
	sleep 0.2
done

E2E_BASE_URL="http://127.0.0.1:$port" npm --prefix web run e2e
