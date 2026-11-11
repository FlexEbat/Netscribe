#!/bin/sh
# Signs in through a real Authentik: PostgreSQL, the server and the worker in containers, a
# provider and an application made by a blueprint, a user made in the Django shell. The service
# runs with auth.oidc pointed at it and curl plays the browser: Authentik has no login form to
# post but a flow API, so the script walks it (identification, password, redirect). Checks that
# a group of Authentik gives the role operator. Needs Docker and ./netscribe (make build).
set -eu

cd "$(dirname "$0")/.."
[ -x ./netscribe ] || { echo "oidc-authentik: build the binary first (make build)" >&2; exit 1; }
command -v docker >/dev/null || { echo "oidc-authentik: Docker is needed" >&2; exit 1; }
command -v python3 >/dev/null || { echo "oidc-authentik: python3 is needed" >&2; exit 1; }

AK_IMAGE="${AK_IMAGE:-ghcr.io/goauthentik/server:2026.8.5}"
akport="${AK_PORT:-19000}"
port="${OIDC_PORT:-18090}"
dir="$(mktemp -d)"
pid=""
tag="netscribe-ak-$$"
net="$tag-net"
trace="$dir/trace.log"
: >"$trace"
say() { printf '%s\n' "$*" >>"$trace"; }
fail() {
	echo "::error title=oidc-authentik::$*"
	echo "oidc-authentik: $*" >&2
	exit 1
}
cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		echo "::notice title=authentik trace::$(grep -v '^{' "$trace" | tail -40 | cut -c1-300 | paste -sd'|' || true)"
		echo "::notice title=authentik server log::$(docker logs "$tag-server" 2>&1 | tail -12 | cut -c1-300 | paste -sd'|' || true)"
		echo "::notice title=authentik worker log::$(docker logs "$tag-worker" 2>&1 | tail -12 | cut -c1-300 | paste -sd'|' || true)"
		echo "::notice title=netscribe sso log::$(tail -6 "$dir/server.log" 2>/dev/null | cut -c1-600 | paste -sd'|' || true)"
	fi
	[ -n "$pid" ] && kill "$pid" 2>/dev/null || true
	docker rm -f "$tag-server" "$tag-worker" "$tag-db" >/dev/null 2>&1 || true
	docker network rm "$net" >/dev/null 2>&1 || true
	rm -rf "$dir"
}
trap cleanup EXIT INT TERM

base="http://127.0.0.1:$port"
ak="http://127.0.0.1:$akport"
issuer="$ak/application/o/netscribe/"
redirect="$base/api/auth/oidc/callback"

docker network create "$net" >/dev/null
docker run -d --name "$tag-db" --network "$net" \
	-e POSTGRES_USER=authentik -e POSTGRES_PASSWORD=ak-db-password -e POSTGRES_DB=authentik \
	postgres:16-alpine >/dev/null
i=0
until docker exec "$tag-db" pg_isready -U authentik >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -gt 60 ] && fail "PostgreSQL did not start"
	sleep 1
done

# The provider, its application, a group and a person. The client secret and the redirect
# address are the ones the service gets below.
mkdir -p "$dir/blueprints"
cat >"$dir/blueprints/netscribe.yaml" <<YAML
version: 1
metadata:
  name: netscribe
entries:
  - model: authentik_core.group
    id: operators
    identifiers:
      name: netscribe operators
  - model: authentik_core.user
    id: omar
    identifiers:
      username: omar
    attrs:
      name: Omar Ops
      email: omar@example.com
      is_active: true
      groups:
        - !KeyOf operators
  - model: authentik_providers_oauth2.oauth2provider
    id: provider
    identifiers:
      name: netscribe
    attrs:
      authorization_flow: !Find [authentik_flows.flow, [slug, default-provider-authorization-implicit-consent]]
      invalidation_flow: !Find [authentik_flows.flow, [slug, default-provider-invalidation-flow]]
      client_type: confidential
      # Empty by default: with no grant type Authentik answers the authorize request with
      # invalid_request ("Invalid grant_type for provider").
      grant_types:
        - authorization_code
        - refresh_token
      client_id: netscribe
      client_secret: ak-test-client-secret
      redirect_uris:
        - matching_mode: strict
          url: $redirect
      signing_key: !Find [authentik_crypto.certificatekeypair, [name, authentik Self-signed Certificate]]
      property_mappings:
        - !Find [authentik_providers_oauth2.scopemapping, [managed, goauthentik.io/providers/oauth2/scope-openid]]
        - !Find [authentik_providers_oauth2.scopemapping, [managed, goauthentik.io/providers/oauth2/scope-email]]
        - !Find [authentik_providers_oauth2.scopemapping, [managed, goauthentik.io/providers/oauth2/scope-profile]]
  - model: authentik_core.application
    identifiers:
      slug: netscribe
    attrs:
      name: Netscribe
      provider: !KeyOf provider
YAML

# A key made now, for this run only.
secret_key=$(head -c 48 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9' | head -c 50)
common="-e AUTHENTIK_SECRET_KEY=$secret_key \
-e AUTHENTIK_POSTGRESQL__HOST=$tag-db -e AUTHENTIK_POSTGRESQL__USER=authentik \
-e AUTHENTIK_POSTGRESQL__PASSWORD=ak-db-password -e AUTHENTIK_POSTGRESQL__NAME=authentik \
-e AUTHENTIK_BOOTSTRAP_PASSWORD=ak-admin-password -e AUTHENTIK_BOOTSTRAP_EMAIL=admin@example.com \
-e AUTHENTIK_ERROR_REPORTING__ENABLED=false -e AUTHENTIK_DISABLE_UPDATE_CHECK=true -e AUTHENTIK_DISABLE_STARTUP_ANALYTICS=true"
# shellcheck disable=SC2086
docker run -d --name "$tag-server" --network "$net" -p "127.0.0.1:$akport:9000" $common \
	-v "$dir/blueprints:/blueprints/custom:ro" "$AK_IMAGE" server >/dev/null
# shellcheck disable=SC2086
docker run -d --name "$tag-worker" --network "$net" $common \
	-v "$dir/blueprints:/blueprints/custom:ro" "$AK_IMAGE" worker >/dev/null

i=0
until curl -fsS "$ak/-/health/ready/" >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -gt 240 ] && fail "Authentik did not become ready in four minutes"
	sleep 1
done
say "authentik is ready after $i s"
# The worker applies the blueprint a little after the server is up.
i=0
until curl -fsS "${issuer}.well-known/openid-configuration" >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -gt 240 ] && fail "the blueprint was not applied in four minutes (no application netscribe)"
	sleep 1
done
say "the application exists after $i s more"
named=$(curl -fsS "${issuer}.well-known/openid-configuration" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("issuer",""))')
say "issuer named: $named"
[ "$named" = "$issuer" ] || fail "Authentik names another issuer than $issuer (it says $named)"

# A password for the person: the blueprint cannot set one.
docker exec "$tag-server" python -m manage shell -c "
from authentik.core.models import User
u = User.objects.get(username='omar'); u.set_password('ak-user-password'); u.save(); print('password set')
" >>"$trace" 2>&1 || fail "could not set the password of omar (see the trace)"

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
    name: Authentik
    provider: authentik
    issuer: $issuer
    clientId: netscribe
    clientSecret: ak-test-client-secret
    redirectUrl: $redirect
    autoCreate: true
    allowedEmailDomains: [example.com]
    defaultRole: viewer
    roleClaim: groups
    roleMap: {"netscribe operators": operator}
YAML
chmod 600 "$dir/netscribe.yaml"
NETSCRIBE_DEV_PASSWORD=oidc-ak-dev-password-1 go run ./cmd/seed --config "$dir/netscribe.yaml" --dev-user
./netscribe serve --config "$dir/netscribe.yaml" >"$dir/server.log" 2>&1 &
pid=$!
i=0
until curl -fsS "$base/healthz" >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 100 ] || ! kill -0 "$pid" 2>/dev/null; then fail "the server did not start"; fi
	sleep 0.2
done

# json FIELD: reads a field of the JSON on stdin ("" when it is missing).
json() { python3 -c 'import json,sys
try:
    d = json.load(sys.stdin)
except Exception:
    d = {}
v = d.get(sys.argv[1], "")
print(v if isinstance(v, str) else json.dumps(v))' "$1"; }

jar="$dir/jar"
# /login sends the browser to the authorize address of Authentik, which sends it to the flow.
landing=$(curl -sS -L -c "$jar" -b "$jar" -o /dev/null -w '%{url_effective}' "$base/api/auth/oidc/login")
say "landing: $landing"
case "$landing" in
"$ak"/if/flow/*) ;;
*) fail "expected the login flow of Authentik, got $landing" ;;
esac
slug=$(printf '%s' "$landing" | sed -n 's#.*/if/flow/\([^/]*\)/.*#\1#p')
query=$(printf '%s' "$landing" | sed -n 's#.*[?]\(.*\)$#\1#p')
# The flow page passes its own query string to the executor as one value, the way the
# interface of Authentik does; without the encoding the "&" splits it and the flow loses its "next".
encoded=$(printf '%s' "$query" | python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.stdin.read().strip(), safe=""))')
exec="$ak/api/v3/flows/executor/$slug/?query=$encoded"
say "executor: $exec"
csrf() { awk '$6 == "authentik_csrf" {print $7}' "$jar" | tail -1; }
step() { # step BODY: posts a JSON body to the executor, prints the answer
	code=$(curl -sS -o "$dir/step.json" -D "$dir/step.head" -w '%{http_code}' -c "$jar" -b "$jar" \
		-H "X-authentik-CSRF: $(csrf)" -H 'Content-Type: application/json' -d "$1" "$exec" 2>>"$trace") || say "the post failed"
	say "post answered $code, csrf cookie of $(csrf | wc -c) bytes, cookies: $(awk 'NF >= 7 {print $6}' "$jar" | paste -sd, -)"
	say "location: $(grep -i "^location:" "$dir/step.head" | cut -c1-200)"
	cat "$dir/step.json" 2>/dev/null || true
}
answer=$(curl -sS -c "$jar" -b "$jar" "$exec")
say "first: $(printf '%s' "$answer" | cut -c1-200)"
comp=$(printf '%s' "$answer" | json component)
n=0
to=""
while [ "$n" -lt 6 ]; do
	n=$((n + 1))
	case "$comp" in
	ak-stage-identification)
		answer=$(step '{"component":"ak-stage-identification","uid_field":"omar","password":"ak-user-password"}') ;;
	ak-stage-password)
		answer=$(step '{"component":"ak-stage-password","password":"ak-user-password"}') ;;
	xak-flow-redirect)
		to=$(printf '%s' "$answer" | json to)
		break ;;
	*) fail "the login flow asked for a stage I do not know: '$comp' (the answer was: $(printf '%s' "$answer" | cut -c1-300))" ;;
	esac
	say "step $n ($comp): $(printf '%s' "$answer" | cut -c1-200)"
	comp=$(printf '%s' "$answer" | json component)
done
[ -n "$to" ] || fail "the login flow did not end in a redirect (last stage '$comp')"
say "redirect: $to"
final=$(curl -sS -L -c "$jar" -b "$jar" -o /dev/null -w '%{url_effective}' "$ak$to")
[ "$final" = "$base/" ] || fail "after the flow the browser should land on $base/, it is at $final"

me=$(curl -sS -b "$jar" -w ' HTTP=%{http_code}' "$base/api/auth/me")
say "me: $me"
case "$me" in *HTTP=200) ;; *) fail "no session after the sign in: $me" ;; esac
echo "$me" | grep -q '"operator"' || fail "the group netscribe operators should give the role operator: $me"

echo "::notice title=oidc-authentik::signed in through Authentik 2026.8: the group netscribe operators gave the role operator"
echo "oidc-authentik: ok"
