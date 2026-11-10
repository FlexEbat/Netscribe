# Единый вход

Netscribe пускает людей через любого провайдера, который говорит на OpenID Connect. Здесь
настроены три самых частых: Keycloak, Authentik и Microsoft Entra ID. Общие настройки описаны в
README, тут то, чем они отличаются.

Как это работает: человека отправляют к провайдеру, он входит там (с вторым фактором самого
провайдера) и возвращается. Netscribe знает человека по паре «издатель и subject» из ID-токена,
а не по e-mail, поэтому сменившийся или переиспользованный адрес не даёт захватить чужую учётную
запись. Роль читается из ID-токена при каждом входе. Локальные учётные записи работают рядом.

Задайте `auth.oidc.provider`: `keycloak`, `authentik` или `entra`. Он проверяет адрес издателя, где
чаще всего ошибаются, а для Entra задаёт claims с адресом. После изменения перезапустите службу:
`auth.oidc` читается при старте.

## Что проверено

| Провайдер | Как |
| --- | --- |
| Keycloak | Настоящий Keycloak в контейнере, в каждом прогоне CI (`make oidc-keycloak`): вход, роль из `realm_access.roles`, роль из `groups`, неверный пароль. |
| Dex | Настоящий Dex в контейнере, в каждом прогоне CI (`make oidc-dex`). |
| Authentik | Против настоящего Authentik не запускалось. Claims, которые он шлёт (`groups` списком имён, `email_verified`), покрыты тестами с такими claims. |
| Entra ID | Против настоящего тенанта не запускалось. Claims, которые он шлёт (нет `email_verified`, адрес в `preferred_username`, `roles`), покрыты тестами с такими claims. |

Если вы попробуете Authentik или Entra и что-то не так, причина есть в журнале аудита (см. конец
страницы).

## Keycloak

1. В realm: Clients, Create client. Client ID `netscribe`, Client authentication включить,
   Standard flow включить. Valid redirect URI: `https://HOST/api/auth/oidc/callback`. Секрет
   скопируйте на вкладке Credentials.
2. Netscribe читает только ID-токен, поэтому нужное надо добавить именно в него. Откройте
   dedicated scope клиента (Client scopes, `netscribe-dedicated`), Add mapper, By configuration:
   - **Group Membership**: Token claim name `groups`, Full group path выключить, Add to ID token
     включить; или
   - **User Realm Role**: Token claim name `realm_access.roles`, Multivalued включить, Add to ID
     token включить.
3. Отметьте e-mail каждого пользователя как подтверждённый (Users, пользователь, Email verified).

```yaml
auth:
  oidc:
    name: Keycloak
    provider: keycloak
    issuer: https://keycloak.example.com/realms/main # realm, без слеша в конце
    clientId: netscribe
    clientSecret: "..."
    redirectUrl: https://netscribe.example.com/api/auth/oidc/callback
    autoCreate: true
    allowedEmailDomains: [example.com]
    roleClaim: realm_access.roles # или groups
    roleMap: { netscribe-admin: admin, netscribe-ops: operator }
```

Старый Keycloak отдаёт realm по пути `/auth/realms/NAME`; в `issuer` пишите тот адрес, который
называет его `/.well-known/openid-configuration`.

## Authentik

1. Applications, Providers, Create, OAuth2/OpenID Provider. Client type Confidential. Redirect URI
   (strict): `https://HOST/api/auth/oidc/callback`. **Signing Key: выберите сертификат** (например
   самоподписанный). Без ключа Authentik подписывает HS256, а Netscribe его не принимает.
2. Scopes: `openid`, `email`, `profile` (claim `groups` приходит вместе с `profile`).
3. Создайте Application с этим провайдером и запомните slug. Издатель:
   `https://HOST/application/o/SLUG/` **со слешем в конце**; это должен быть тот же текст, что в
   discovery-документе Authentik.

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

1. App registrations, New registration. Supported account types: **только этот каталог**
   (один тенант). Redirect URI, платформа Web: `https://HOST/api/auth/oidc/callback`.
2. Certificates and secrets, New client secret. Сразу скопируйте **Value** (не Secret ID).
3. Token configuration, Add optional claim, тип токена ID, добавьте `email`. (Без него адрес
   берётся из `preferred_username`, если тот похож на адрес.)
4. Роли: App roles, Create app role (Value `Netscribe.Admin`, Users/Groups), затем Enterprise
   applications, приложение, Users and groups, назначьте людей. В ID-токене появится список
   `roles`. Роли лучше давать так. Claim `groups` содержит object ID, а у человека более чем в
   200 группах списка нет вовсе (Netscribe не обращается к Graph, чтобы его прочитать).
5. Издатель: `https://login.microsoftonline.com/TENANT-ID/v2.0`, где TENANT-ID это Directory
   (tenant) ID со страницы Overview, GUID. Не `common`, не `organizations`, не `consumers` и не
   доменное имя: Netscribe их отклоняет, потому что по ним выдаются токены людям из чужих
   тенантов.

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

Entra не присылает `email_verified`, поэтому при `provider: entra` Netscribe его не требует. Это
безопасно только потому, что издатель это один тенант: человек из другого тенанта войти не может,
а адрес служит лишь для имени учётной записи и проверки `allowedEmailDomains`, но не для поиска
учётной записи.

## Остальные настройки

- `emailClaim`: claim с адресом, если у провайдера он называется иначе.
- `requireVerifiedEmail: false`: для провайдера, который сам владеет адресами и не шлёт
  `email_verified`. Включайте только для провайдера, к которому посторонние не могут присоединиться.
- `roleClaim` может быть путём по вложенным объектам, `realm_access.roles`. Имя с точками, например
  `https://example.com/roles`, сначала пробуется целиком.
- `defaultRole` (`viewer` или `operator`, никогда `admin`) получает тот, чей claim ни во что не
  отображается. Администратор появляется только из `roleMap`.

## Если вход не удался

Человек видит страницу входа с общим сообщением, а причина лежит в журнале аудита как
`oidc_login_failed`, в поле detail:

| Detail | Значение |
| --- | --- |
| `no cookie`, `bad cookie`, `state`, `code` | Браузер вернулся не тем путём, каким ушёл (вторая вкладка, старая страница). Повторите. |
| `provider refused` | Провайдер ответил ошибкой, например человеку нельзя пользоваться приложением. |
| `exchange` | Код отклонён на token endpoint: неверный секрет клиента, неверный redirect URI, повторно использованный код. |
| `id token` | Токен недействителен: отличаются издатель или client ID, он просрочен или алгоритм подписи не RS256 (Authentik: выберите ключ подписи). |
| `nonce`, `subject`, `claims` | В токене нет того, что в нём должно быть. |
| `no email` | Нет адреса в `email` (Entra: добавьте optional claim). |
| `email not verified` | Провайдер говорит, что адрес не подтверждён. |
| `no account` | `autoCreate` выключен, а учётной записи нет. |
| `domain` | Адреса нет в `allowedEmailDomains`. |
| `disabled` | Администратор отключил учётную запись. |
| `name taken` | Имя, сделанное для учётной записи, уже занято. |

Тот, кого нет в карте ролей, получает `defaultRole`. У учётной записи единого входа нет пароля;
администратор может задать его на странице пользователей, если человеку нужны резервная копия или
отключение второго фактора, которые спрашивают пароль.
