#!/bin/sh
# Signs in through a real Keycloak, in a container, with a realm that is imported at start. The
# service runs with auth.oidc pointed at it and curl plays the browser: it follows the redirects,
# posts the login form of Keycloak and lands on the service with a session. Two phases: the role
# comes from a nested claim (realm_access.roles), then from the groups claim. Needs Docker and
# ./netscribe (make build).
set -eu

cd "$(dirname "$0")/.."
[ -x ./netscribe ] || { echo "oidc-keycloak: build the binary first (make build)" >&2; exit 1; }
command -v docker >/dev/null || { echo "oidc-keycloak: Docker is needed" >&2; exit 1; }

KC_IMAGE="${KC_IMAGE:-quay.io/keycloak/keycloak:26.0.7}"
kcport="${KC_PORT:-18081}"
port="${OIDC_PORT:-18090}"
dir="$(mktemp -d)"
pid=""
name="netscribe-kc-$$"
fail() {
	echo "::error title=oidc-keycloak::$*"
	echo "oidc-keycloak: $*" >&2
	exit 1
}
cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		echo "::notice title=keycloak log::$(docker logs "$name" 2>&1 | tail -15 | paste -sd'|' || true)"
		echo "::notice title=server log::$(tail -15 "$dir/server.log" 2>/dev/null | paste -sd'|' || true)"
	fi
	[ -n "$pid" ] && kill "$pid" 2>/dev/null || true
	docker rm -f "$name" >/dev/null 2>&1 || true
	rm -rf "$dir"
}
trap cleanup EXIT INT TERM

issuer="http://127.0.0.1:$kcport/realms/netscribe"
redirect="http://127.0.0.1:$port/api/auth/oidc/callback"
base="http://127.0.0.1:$port"

# The realm: one confidential client with a mapper for the groups and one for the realm roles
# (both into the ID token, which is the only token the service reads), and two people.
cat >"$dir/realm.json" <<JSON
{
  "realm": "netscribe",
  "enabled": true,
  "clients": [
    {
      "clientId": "netscribe",
      "enabled": true,
      "protocol": "openid-connect",
      "publicClient": false,
      "secret": "kc-test-client-secret",
      "standardFlowEnabled": true,
      "redirectUris": ["$redirect"],
      "protocolMappers": [
        {
          "name": "groups",
          "protocol": "openid-connect",
          "protocolMapper": "oidc-group-membership-mapper",
          "config": {"claim.name": "groups", "full.path": "false", "id.token.claim": "true", "access.token.claim": "true", "userinfo.token.claim": "true"}
        },
        {
          "name": "realm roles",
          "protocol": "openid-connect",
          "protocolMapper": "oidc-usermodel-realm-role-mapper",
          "config": {"claim.name": "realm_access.roles", "multivalued": "true", "jsonType.label": "String", "id.token.claim": "true", "access.token.claim": "true", "userinfo.token.claim": "true"}
        }
      ]
    }
  ],
  "roles": {"realm": [{"name": "netscribe-admin"}]},
  "groups": [{"name": "netscribe-ops"}],
  "users": [
    {
      "username": "jane", "email": "jane@example.com", "emailVerified": true, "enabled": true,
      "firstName": "Jane", "lastName": "Admin",
      "credentials": [{"type": "password", "value": "password", "temporary": false}],
      "realmRoles": ["netscribe-admin"]
    },
    {
      "username": "omar", "email": "omar@example.com", "emailVerified": true, "enabled": true,
      "firstName": "Omar", "lastName": "Ops",
      "credentials": [{"type": "password", "value": "password", "temporary": false}],
      "groups": ["/netscribe-ops"]
    }
  ]
}
JSON
docker run -d --name "$name" -p "127.0.0.1:$kcport:8080" \
	-e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin-test-password \
	-v "$dir/realm.json:/opt/keycloak/data/import/realm.json:ro" \
	"$KC_IMAGE" start-dev --import-realm >/dev/null

i=0
until curl -fsS "$issuer/.well-known/openid-configuration" >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -gt 400 ] && fail "Keycloak did not start in two minutes"
	sleep 0.5
done
curl -fsS "$issuer/.well-known/openid-configuration" | grep -q "\"issuer\":\"$issuer\"" ||
	fail "Keycloak names another issuer than $issuer"

# start_service ROLE_CLAIM ROLE_MAP_YAML DATABASE_NAME: the service, with a new database.
start_service() {
	[ -n "$pid" ] && { kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; }
	cat >"$dir/netscribe.yaml" <<YAML
listen: 127.0.0.1:$port
database: $dir/$3.db
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
    name: Keycloak
    provider: keycloak
    issuer: $issuer
    clientId: netscribe
    clientSecret: kc-test-client-secret
    redirectUrl: $redirect
    autoCreate: true
    allowedEmailDomains: [example.com]
    defaultRole: viewer
    roleClaim: $1
    roleMap: $2
YAML
	chmod 600 "$dir/netscribe.yaml"
	NETSCRIBE_DEV_PASSWORD=oidc-kc-dev-password-1 go run ./cmd/seed --config "$dir/netscribe.yaml" --dev-user
	./netscribe serve --config "$dir/netscribe.yaml" >"$dir/server.log" 2>&1 &
	pid=$!
	j=0
	until curl -fsS "$base/healthz" >/dev/null 2>&1; do
		j=$((j + 1))
		if [ "$j" -gt 100 ] || ! kill -0 "$pid" 2>/dev/null; then fail "the server did not start"; fi
		sleep 0.2
	done
}

# sign_in USER PASSWORD JAR: prints the page where the browser ends.
sign_in() {
	login=$(curl -sS -L -c "$3" -b "$3" "$base/api/auth/oidc/login")
	action=$(printf '%s' "$login" | grep -o 'action="[^"]*"' | head -1 | sed 's/^action="//; s/"$//; s/&amp;/\&/g')
	[ -n "$action" ] || fail "the login page of Keycloak has no form"
	curl -sS -L -c "$3" -b "$3" -o /dev/null -w '%{url_effective}' \
		--data-urlencode "username=$1" --data-urlencode "password=$2" "$action"
}

# Phase one: the role from a nested claim.
start_service "realm_access.roles" "{netscribe-admin: admin}" one
curl -fsS "$base/api/auth/providers" | grep -q '"Keycloak"' || fail "the providers answer does not name Keycloak"
final=$(sign_in jane password "$dir/jar1")
[ "$final" = "$base/" ] || fail "jane should land on $base/, she is at $final"
me=$(curl -sS -b "$dir/jar1" -w ' HTTP=%{http_code}' "$base/api/auth/me")
case "$me" in *HTTP=200) ;; *) fail "no session for jane: $me" ;; esac
echo "$me" | grep -q '"admin"' || fail "the realm role netscribe-admin should give the role admin: $me"
final=$(sign_in jane password "$dir/jar1b")
[ "$final" = "$base/" ] || fail "jane's second sign in ended at $final"
bad=$(sign_in jane wrong "$dir/jar1c")
case "$bad" in "$base"*) fail "a wrong password reached the service: $bad" ;; esac
[ "$(curl -sS -b "$dir/jar1c" -o /dev/null -w '%{http_code}' "$base/api/auth/me")" = "401" ] || fail "a wrong password opened a session"

# Phase two: the role from the groups claim.
start_service "groups" "{netscribe-ops: operator}" two
final=$(sign_in omar password "$dir/jar2")
[ "$final" = "$base/" ] || fail "omar should land on $base/, he is at $final"
me=$(curl -sS -b "$dir/jar2" -w ' HTTP=%{http_code}' "$base/api/auth/me")
case "$me" in *HTTP=200) ;; *) fail "no session for omar: $me" ;; esac
echo "$me" | grep -q '"operator"' || fail "the group netscribe-ops should give the role operator: $me"

echo "::notice title=oidc-keycloak::signed in through Keycloak: a realm role (realm_access.roles) gave admin, a group gave operator, a second sign in found the same user, a wrong password was refused"
echo "oidc-keycloak: ok"
