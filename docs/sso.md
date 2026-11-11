# Single sign-on

Netscribe signs people in through any identity provider that speaks OpenID Connect. This page
sets up the three that most teams have: Keycloak, Authentik and Microsoft Entra ID. The general
settings are in the README; here is what differs.

How it works: the person is sent to the provider, signs in there (with the provider's own second
factor), and comes back. Netscribe knows the person by the issuer and the subject of the ID token,
never by the e-mail address, so a changed or re-used address cannot take over an account. The
role is read from the ID token at every sign in. Local accounts keep working beside it.

Set `auth.oidc.provider` to `keycloak`, `authentik` or `entra`. It checks the address of the issuer,
where the mistakes are made, and for Entra it sets the e-mail claims. Restart the service after
a change: `auth.oidc` is read at start.

## What was tested

| Provider | How |
| --- | --- |
| Keycloak | A real Keycloak in a container, in every CI run (`make oidc-keycloak`): sign in, a role from `realm_access.roles`, a role from `groups`, a wrong password. |
| Dex | A real Dex in a container, in every CI run (`make oidc-dex`). |
| Authentik | A real Authentik 2026.8 in containers, in every CI run (`make oidc-authentik`): the login flow, the sign in, and the role `operator` from the group `netscribe operators`. |
| Entra ID | Not run against a real tenant. The claims it sends (no `email_verified`, the address in `preferred_username`, `roles`) are covered by tests with those claims. |

If you try Authentik or Entra and something is wrong, the reason is in the audit log (see the end
of this page).

## Keycloak

1. In the realm, Clients, Create client. Client ID `netscribe`, Client authentication on, Standard
   flow on. Valid redirect URI: `https://HOST/api/auth/oidc/callback`. Copy the secret from the
   Credentials tab.
2. Netscribe reads the ID token only, so what you want in it must be added to it. Open the
   client's dedicated scope (Client scopes, `netscribe-dedicated`), Add mapper, By configuration:
   - **Group Membership**: Token claim name `groups`, Full group path off, Add to ID token on; or
   - **User Realm Role**: Token claim name `realm_access.roles`, Multivalued on, Add to ID token on.
3. Mark the e-mail address of each user as verified (Users, the user, Email verified).

```yaml
auth:
  oidc:
    name: Keycloak
    provider: keycloak
    issuer: https://keycloak.example.com/realms/main # the realm, no trailing slash
    clientId: netscribe
    clientSecret: "..."
    redirectUrl: https://netscribe.example.com/api/auth/oidc/callback
    autoCreate: true
    allowedEmailDomains: [example.com]
    roleClaim: realm_access.roles # or groups
    roleMap: { netscribe-admin: admin, netscribe-ops: operator }
```

An older Keycloak serves the realm under `/auth/realms/NAME`; use the address that its
`/.well-known/openid-configuration` names as `issuer`.

## Authentik

1. Applications, Providers, Create, OAuth2/OpenID Provider. Client type Confidential. Redirect URI
   (strict): `https://HOST/api/auth/oidc/callback`. **Signing Key: choose a certificate** (for
   example the self-signed one). With no key Authentik signs with HS256, which Netscribe does not
   accept.
2. Scopes: `openid`, `email`, `profile` (the `groups` claim comes with `profile`).
3. Create an Application with that provider and note its slug. The issuer is
   `https://HOST/application/o/SLUG/` **with the slash at the end**; it must be the same text as
   Authentik's discovery document says.

```yaml
auth:
  oidc:
    name: Authentik
    provider: authentik
    issuer: https://authentik.example.com/application/o/netscribe/
    clientId: "..."
    clientSecret: "..."
    redirectUrl: https://netscribe.example.com/api/auth/oidc/callback
    autoCreate: true
    allowedEmailDomains: [example.com]
    roleClaim: groups
    roleMap: { "authentik Admins": admin, "netscribe operators": operator }
```

## Microsoft Entra ID

1. App registrations, New registration. Supported account types: **this organizational directory
   only** (one tenant). Redirect URI, platform Web: `https://HOST/api/auth/oidc/callback`.
2. Certificates and secrets, New client secret. Copy the **Value** at once (not the Secret ID).
3. Token configuration, Add optional claim, token type ID, add `email`. (Without it the address is
   read from `preferred_username` when that looks like an address.)
4. Roles: App roles, Create app role (Value `Netscribe.Admin`, allowed member types Users/Groups),
   then Enterprise applications, the app, Users and groups, assign people. The ID token then has a
   `roles` list. This is the way to give roles. The `groups` claim holds object IDs and, for a
   person in more than 200 groups, no list at all (Netscribe does not call the Graph to read it).
5. The issuer is `https://login.microsoftonline.com/TENANT-ID/v2.0` with the Directory (tenant) ID
   from the Overview page, a GUID. Not `common`, `organizations`, `consumers` or a domain name:
   Netscribe refuses them, because those issue tokens for people of other tenants.

```yaml
auth:
  oidc:
    name: Microsoft
    provider: entra
    issuer: https://login.microsoftonline.com/72f988bf-86f1-41af-91ab-2d7cd011db47/v2.0
    clientId: "..." # Application (client) ID
    clientSecret: "..."
    redirectUrl: https://netscribe.example.com/api/auth/oidc/callback
    autoCreate: true
    allowedEmailDomains: [example.com]
    roleClaim: roles
    roleMap: { Netscribe.Admin: admin, Netscribe.Operator: operator }
```

Entra sends no `email_verified`, so for `provider: entra` Netscribe does not ask for it. That is safe
only because the issuer is one tenant: a person of another tenant cannot sign in, and an address
is used only to name the account and to check `allowedEmailDomains`, never to find an account.

The same holds for `provider: authentik`: its default `email` scope mapping sends a fixed
`email_verified: false`, so Netscribe does not ask for it either. If you change the mapping to send
a real value and want it checked, set `requireVerifiedEmail: true`.

## Other settings

- `emailClaim`: the claim that holds the address, for a provider that uses another name.
- `requireVerifiedEmail: false`: for a provider that owns its addresses and sends no
  `email_verified`. Say it only for a provider that outsiders cannot join.
- `roleClaim` may be a path through nested objects, `realm_access.roles`. A name with dots, such
  as `https://example.com/roles`, is tried as a whole first.
- `defaultRole` (`viewer` or `operator`, never `admin`) is the role of a person whose claim maps to
  nothing. An administrator comes only from `roleMap`.

## When a sign in fails

The person sees the sign in page with a general message; the reason is in the audit log as
`oidc_login_failed`, in the detail:

| Detail | Meaning |
| --- | --- |
| `no cookie`, `bad cookie`, `state`, `code` | The browser did not return the way it left (a second tab, an old page). Try again. |
| `provider refused` | The provider answered with an error, for example the person is not allowed to use the application. |
| `exchange` | The code was refused at the token endpoint: a wrong client secret, a wrong redirect URI, a reused code. |
| `id token` | The token is not valid: the issuer or the client ID differs, it has expired, or the signing algorithm is not RS256 (Authentik: choose a signing key). |
| `nonce`, `subject`, `claims` | The token lacks what a token must have. |
| `no email` | No address in `email` (Entra: add the optional claim). |
| `email not verified` | The provider says the address is not verified. |
| `no account` | `autoCreate` is off and the person has no account. |
| `domain` | The address is not in `allowedEmailDomains`. |
| `disabled` | An administrator disabled the account. |
| `name taken` | The name made for the account exists already. |

A person who is not in the role map gets `defaultRole`. An account made by single sign-on has no
password; an administrator can set one in the Users page if the person must take a backup or turn
off a second factor, which ask for a password.
