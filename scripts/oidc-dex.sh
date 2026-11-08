#!/bin/sh
# Signs in through a real identity provider: Dex, in a container, with one static user. The
# service runs with auth.oidc pointed at it, and curl plays the browser: it follows the
# redirects, posts the login form of Dex and lands on the service with a session. Needs
# Docker and ./netscribe (make build). Nothing leaves the machine but the image pull.
set -eu

cd "$(dirname "$0")/.."
[ -x ./netscribe ] || { echo "oidc-dex: build the binary first (make build)" >&2; exit 1; }
command -v docker >/dev/null || { echo "oidc-dex: Docker is needed" >&2; exit 1; }

DEX_IMAGE="${DEX_IMAGE:-ghcr.io/dexidp/dex:v2.41.1}"
dexport="${DEX_PORT:-15556}"
port="${OIDC_PORT:-18090}"
dir="$(mktemp -d)"
pid=""
name="netscribe-dex-$$"
fail() {
	echo "::error title=oidc-dex::$*"
	echo "oidc-dex: $*" >&2
	exit 1
}
cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		echo "::notice title=dex log::$(docker logs "$name" 2>&1 | tail -15 | paste -sd'|' || true)"
		echo "::notice title=server log::$(tail -15 "$dir/server.log" 2>/dev/null | paste -sd'|' || true)"
	fi
	[ -n "$pid" ] && kill "$pid" 2>/dev/null || true
	docker rm -f "$name" >/dev/null 2>&1 || true
	rm -rf "$dir"
}
trap cleanup EXIT INT TERM

issuer="http://127.0.0.1:$dexport/dex"
redirect="http://127.0.0.1:$port/api/auth/oidc/callback"

# bcrypt of "password", the one the Dex examples use.
cat >"$dir/dex.yaml" <<YAML
issuer: $issuer
storage:
  type: memory
web:
  http: 0.0.0.0:5556
oauth2:
  skipApprovalScreen: true
enablePasswordDB: true
staticClients:
  - id: netscribe
    name: Netscribe
    secret: dex-test-client-secret
    redirectURIs:
      - $redirect
staticPasswords:
  - email: jane@example.com
    hash: "\$2a\$10\$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W"
    username: jane
    userID: "08a8684b-db88-4b73-90a9-3cd1661f5466"
YAML
docker run -d --name "$name" -p "127.0.0.1:$dexport:5556" -v "$dir/dex.yaml:/etc/dex/config.yaml:ro" \
	"$DEX_IMAGE" dex serve /etc/dex/config.yaml >/dev/null

i=0
until curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -gt 100 ] && fail "Dex did not start"
	sleep 0.3
done

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
auth:
  oidc:
    name: Dex
    issuer: $issuer
    clientId: netscribe
    clientSecret: dex-test-client-secret
    redirectUrl: $redirect
    autoCreate: true
    allowedEmailDomains: [example.com]
    defaultRole: viewer
YAML
chmod 600 "$dir/netscribe.yaml"
NETSCRIBE_DEV_PASSWORD=oidc-dex-dev-password-1 go run ./cmd/seed --config "$dir/netscribe.yaml" --dev-user
./netscribe serve --config "$dir/netscribe.yaml" >"$dir/server.log" 2>&1 &
pid=$!
i=0
until curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 100 ] || ! kill -0 "$pid" 2>/dev/null; then fail "the server did not start"; fi
	sleep 0.2
done

base="http://127.0.0.1:$port"
curl -fsS "$base/api/auth/providers" | grep -q '"Dex"' || fail "the providers answer does not name Dex"

# A good sign in: /login -> Dex -> its login form -> the callback -> /.
jar="$dir/jar"
login_page=$(curl -sS -L -c "$jar" -b "$jar" -o /dev/null -w '%{url_effective}' "$base/api/auth/oidc/login")
case "$login_page" in
"$issuer"/auth/local/login*) ;;
*) fail "expected the login form of Dex, got $login_page" ;;
esac
final=$(curl -sS -L -c "$jar" -b "$jar" -o /dev/null -w '%{url_effective}' \
	--data-urlencode "login=jane@example.com" --data-urlencode "password=password" "$login_page")
[ "$final" = "$base/" ] || fail "after the login form the browser should land on $base/, it is at $final"

me=$(curl -sS -b "$jar" -w ' HTTP=%{http_code}' "$base/api/auth/me")
case "$me" in
*HTTP=200) ;;
*) fail "no session after the sign in: $me" ;;
esac
echo "$me" | grep -q '"viewer"' || fail "the new user should be a viewer: $me"
echo "$me" | grep -q 'jane' || fail "the new user should be named from the e-mail: $me"

# A second sign in finds the same user (no second account).
jar2="$dir/jar2"
page2=$(curl -sS -L -c "$jar2" -b "$jar2" -o /dev/null -w '%{url_effective}' "$base/api/auth/oidc/login")
final2=$(curl -sS -L -c "$jar2" -b "$jar2" -o /dev/null -w '%{url_effective}' \
	--data-urlencode "login=jane@example.com" --data-urlencode "password=password" "$page2")
[ "$final2" = "$base/" ] || fail "the second sign in ended at $final2"
me2=$(curl -sS -b "$jar2" "$base/api/auth/me")
u1=$(echo "$me" | sed -n 's/.*"username":"\([^"]*\)".*/\1/p')
u2=$(echo "$me2" | sed -n 's/.*"username":"\([^"]*\)".*/\1/p')
[ -n "$u1" ] && [ "$u1" = "$u2" ] || fail "two sign ins gave two users: '$u1' and '$u2'"

# A wrong password at Dex never reaches the service.
jar3="$dir/jar3"
page3=$(curl -sS -L -c "$jar3" -b "$jar3" -o /dev/null -w '%{url_effective}' "$base/api/auth/oidc/login")
bad=$(curl -sS -L -c "$jar3" -b "$jar3" -o /dev/null -w '%{url_effective}' \
	--data-urlencode "login=jane@example.com" --data-urlencode "password=wrong" "$page3")
case "$bad" in
"$base"*) fail "a wrong password reached the service: $bad" ;;
esac
code=$(curl -sS -b "$jar3" -o /dev/null -w '%{http_code}' "$base/api/auth/me")
[ "$code" = "401" ] || fail "a wrong password opened a session ($code)"

echo "::notice title=oidc-dex::signed in through Dex as $u1; a second sign in found the same user; a wrong password was refused"
echo "oidc-dex: ok ($u1)"
