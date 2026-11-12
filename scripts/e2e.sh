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
	# The timings of the specs (see note() in web/e2e/helpers.ts), as one CI notice: the file is deleted below.
	[ -s "$dir/timings.log" ] && echo "::notice title=e2e timings::$(paste -sd'|' "$dir/timings.log")" || true
	# The phases of every PDF the server printed (internal/render/pdf.go), as one notice.
	grep -E "pdf |export built" "$dir/server.log" 2>/dev/null | sed 's/^time=[^ ]* //' | paste -sd'|' | sed 's/^\(.\)/::notice title=pdf phases::\1/' | grep . || true
	[ -n "$pid" ] && kill "$pid" 2>/dev/null || true
	rm -rf "$dir"
}
trap cleanup EXIT INT TERM

port="${E2E_PORT:-18080}"
NETSCRIBE_DEV_PASSWORD="${NETSCRIBE_DEV_PASSWORD:-e2e-dev-password-12}"
export NETSCRIBE_DEV_PASSWORD
E2E_TIMINGS="$dir/timings.log"
export E2E_TIMINGS

# E2E_LAB=1 scans the network of deploy/compose.lab.yaml, which must be running, and runs only
# lab.spec.ts. Every other mode scans nothing: the schedule is off.
targets="192.168.1.0/24"
if [ -n "${E2E_LAB:-}" ]; then
	targets="172.30.0.0/24"
	export E2E_LAB
fi

cat >"$dir/netscribe.yaml" <<YAML
listen: 127.0.0.1:$port
database: $dir/netscribe.db
scan:
  targets: [$targets]
  interval: 0
snmp:
  communities: [public]
  timeout: 2s
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
export:
  # The runner restricts user namespaces, so Chrome's sandbox fails after up to a minute and the
  # print of the large network runs past the limit of the server. The fallback is covered by the
  # Go tests of internal/render.
  noSandbox: true
YAML
chmod 600 "$dir/netscribe.yaml"

# E2E_DEVICES=600 seeds a generated network and runs only the large network specs.
if [ -n "${E2E_DEVICES:-}" ]; then
	go run ./cmd/seed --config "$dir/netscribe.yaml" --dev-user --devices "$E2E_DEVICES"
	export E2E_LARGE=1
else
	go run ./cmd/seed --config "$dir/netscribe.yaml" --dev-user
fi
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
