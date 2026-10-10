# tech.md: генератор сетевой документации (рабочее имя netdoc)

**Версия: v0 (черновик)** (2026-10-07)

Changelog:
- v0 (правка 2) - добавлены раздел 9 «Безопасность», аутентификация, роли и многопользовательский режим, ручное добавление устройств, расширенное обнаружение. Слайсы 2, 5, 6, 8 новые, остальные сдвинуты.
- v0 - черновик для финализации. Контракты не заморожены. Раздел 18 перечисляет открытые вопросы. После ответов на них версия поднимается до v1 и контракты замораживаются.

---

## 0. Как читать этот файл

Это источник истины проекта. Читай его перед каждой задачей и подчиняйся дословно.

- Имена таблиц, полей, типов, функций и маршрутов берутся отсюда. Свои варианты не придумывай.
- Нужного контракта здесь нет → СТОП, выдай блок `CONTRACT GAP` (раздел 14). Код с выдуманным типом не пиши.
- Файл меняет только владелец проекта. Каждое изменение контракта поднимает версию сверху файла.
- Работаешь один слайс за заход. Стадии и слайсы - раздел 16, иди по нему сверху вниз.
- Пока версия v0, контракты можно править. С версии v1 они заморожены.

---

## 1. Проект

Панель для документирования локальной сети. Сервис сканирует сеть по расписанию и по кнопке (ARP, SNMP, LLDP, SSH, Docker), хранит инвентарь устройств, IP-адреса, связи, сервисы и контейнеры, рисует топологию и отдаёт документацию в Markdown, HTML, PDF и Draw.io. Один бинарник, веб-панель встроена. Устройства находятся автоматически, а то, что обнаружить нельзя, добавляется вручную. Панель многопользовательская: вход по логину и паролю, три роли (`viewer`, `operator`, `admin`), журнал аудита.

Для кого: малый и средний бизнес без сетевой документации и студенты, которым нужно её сдавать.

Зачем: документация устаревает. Сервис пересобирает её из живой сети и показывает, что изменилось с прошлого скана.

Цель v1: работающая панель с топологией, инвентарём, контейнерами, экспортом и разграничением доступа, которая проходит гейт проверок. Не платформа: нет мультитенантности (несколько организаций в одном экземпляре), алертинга, мониторинга трафика и управления устройствами.

Границы безопасности (действуют во всех слайсах):

- Сервис сканирует только цели из конфига. По умолчанию разрешены только частные диапазоны RFC 1918. Публичные адреса включаются явным флагом `allowPublicTargets`.
- Сервис только читает. SNMP: GET, GETNEXT, GETBULK, без SET. SSH: фиксированный набор команд на чтение. Docker: только запросы списков и поток событий.
- Доступ к панели только после входа. Права проверяются на сервере для каждого маршрута. Панель слушает `127.0.0.1` по умолчанию. Полный перечень параметров безопасности в разделе 9, он обязателен для каждого слайса.
- Секреты (SNMP community, пути к ключам) не попадают в логи, API и экспорт.

---

## 2. Стек

Бэкенд:

- Go (актуальная стабильная версия, фиксируется в `go.mod`), в v1 только Linux
- `net/http` + `chi`, логи через `log/slog`
- SQLite через `modernc.org/sqlite` (чистый Go, без CGO), миграции `goose`, запросы через `sqlc`
- `gosnmp` (SNMP v2c), `golang.org/x/crypto/ssh`, официальный Docker Go client, `fsnotify`, `gopkg.in/yaml.v3`
- `golang.org/x/crypto/argon2` (пароли), `golang.org/x/time/rate` (лимиты попыток входа), `crypto/tls` из стандартной библиотеки
- `chromedp` только для экспорта в PDF

Фронтенд:

- React, TypeScript strict, Vite
- Tailwind CSS, shadcn/ui (компоненты копируются в репозиторий)
- React Flow (`@xyflow/react`) для топологии, Recharts для графиков
- TanStack Query для серверного состояния, React Router для маршрутов
- Собранный фронтенд встраивается в бинарник через `go:embed`

Тесты и проверки: `go test`, `gofmt`, `go vet`, `golangci-lint` (включая `gosec`), `govulncheck`, vitest, eslint (с `jsx-a11y`), prettier, `tsc`, `npm audit`.

Поставка: нативный бинарник (основной способ) и Docker-образ с `compose.yaml`.

Без ORM, без PostgreSQL в v1, без Redux и других стейт-менеджеров, без CSS-in-JS, без GraphQL и gRPC, без брокеров сообщений. Один процесс, один бинарник, один файл базы. Аутентификация своя (пользователи и сессии в SQLite), внешних провайдеров в v1 нет.

---

## 3. Структура папок

```
cmd/
  netdoc/
    main.go                   подкоманды serve, scan, devices, export, user
  seed/
    main.go                   демо-сеть для разработки и скриншотов, учётка dev из NETDOC_DEV_PASSWORD
internal/
  config/
    config.go                 структуры конфига, загрузка YAML, Validate, тип Secret
    watch.go                  fsnotify и SIGHUP, атомарная подмена конфига
  auth/
    password.go               argon2id: хеш, проверка, правила пароля
    session.go                сессии и токены доступа: выпуск, проверка, отзыв
    rbac.go                   роли, права, таблица «роль - права»
    ratelimit.go              ограничение попыток входа
  model/
    model.go                  доменные типы и константы (раздел 4)
  store/
    store.go                  Open(path), интерфейс Repo, ErrNotFound
    apply.go                  ApplyResult: слияние результата скана с базой, запись changes
    migrations/               goose, файлы NNNN_<тема>.sql, одна миграция на слайс, номера по порядку слайсов
    queries/                  SQL для sqlc
    db/                       код sqlc, сгенерирован, руками не править
  collector/
    collector.go              интерфейс Collector, Input, Result
    arp.go                    прогрев ARP и чтение /proc/net/arp
    snmp.go                   sysName, sysDescr, ifTable
    lldp.go                   соседи из LLDP-MIB
    snmparp.go                ARP-таблицы и таблицы пересылки (FDB) маршрутизаторов и коммутаторов
    probe.go                  ICMP и TCP-зондирование маршрутизируемых подсетей
    classify.go               вид устройства по правилам, правила в classify_rules.csv
    oui.go, oui.csv           вендор по MAC (встроенная таблица)
    services.go               TCP connect-скан списка портов
    ssh.go                    фиксированные команды на чтение
    docker.go                 контейнеры, сети, события
    scanner.go                оркестрация скана, расписание, запись в store, события
  events/
    hub.go                    шина событий для SSE
  render/
    layout.go                 раскладка по слоям с учётом сохранённых координат
    svg.go                    SVG-диаграмма
    mermaid.go                Mermaid-граф
    markdown.go
    drawio.go
    html.go
    pdf.go
    templates/                html/template
  api/
    router.go                 таблица маршрутов (метод, путь, право, обработчик), раздача SPA
    middleware.go             заголовки безопасности, лимиты, аутентификация, CSRF, проверка прав, аудит
    auth.go, users.go, tokens.go, audit.go
    devices.go, links.go, scans.go, topology.go, containers.go, services.go, changes.go
    export.go
    sse.go
web/
  embed.go                    go:embed dist
  src/
    main.tsx
    api/
      client.ts               fetch-обёртка и типы ответов (зеркало раздела 4)
      events.ts               подписка на SSE
    components/
      ui/                     примитивы shadcn/ui
      DeviceNode.tsx, TopologyCanvas.tsx, DevicePanel.tsx
      ScanBar.tsx, ExportMenu.tsx, StatusBadge.tsx, EmptyState.tsx
      DeviceForm.tsx, RoleBadge.tsx, UserMenu.tsx, RequirePermission.tsx
    pages/
      Login.tsx, Account.tsx, Topology.tsx, Inventory.tsx, Containers.tsx, Services.tsx, Changes.tsx
      Users.tsx, Audit.tsx
    lib/                      чистая логика без React (сортировка IP, фильтры, форматирование, permissions.ts)
  public/
    theme-init.js             выбор темы до отрисовки, внешний файл (inline-скриптов нет)
  tests/
deploy/
  Dockerfile
  compose.yaml                сервис в бою
  compose.lab.yaml            тестовая лаборатория
  lab/                        конфиги лаборатории
testdata/                     фикстуры snmpwalk, /proc/net/arp, golden-файлы
netdoc.example.yaml
Makefile
.air.toml, .golangci.yml, sqlc.yaml
SECURITY.md
```

Правила размещения:

- SQL живёт только в `internal/store/migrations` и `internal/store/queries`. Код в `internal/store/db` генерирует sqlc.
- Пакет `collector` не импортирует `store`. Коллектор возвращает `Result`, а `scanner` записывает его через `Repo.ApplyResult`.
- Пакет `render` не импортирует `collector` и `api`. Он принимает `model.Topology` и `[]model.Change`.
- Пакет `api` не содержит SQL и не вызывает коллекторы напрямую. Он использует `store.Repo`, `scanner` и `render`.
- Фронтенд ходит к серверу только через `web/src/api/`. Компоненты не вызывают `fetch` сами.
- Чистая логика (парсеры, раскладка, валидация, сортировка IP) лежит в функциях без сети и базы, чтобы тестироваться без них.
- Значения типа `config.Secret` не логируются, не сериализуются в JSON и не попадают в экспорт.
- Маршруты регистрируются только через таблицу в `internal/api/router.go` (метод, путь, право, обработчик). Прямая регистрация через `mux.Handle` запрещена. Маршрут без права не регистрируется, публичные маршруты помечаются явно.
- Пакет `auth` не импортирует `api` и `store`: данные он получает через интерфейс `AuthRepo`.
- Права проверяет только middleware. Обработчики роль не сравнивают.
- Фронтенд скрывает недоступные действия по `web/src/lib/permissions.ts`. Это удобство, защита работает на сервере.

---

## 4. Контракт данных (черновик)

Время хранится в UTC, в SQLite это TEXT в формате RFC 3339. Имена колонок в базе в `snake_case`, поля Go в `CamelCase`, поля JSON в `camelCase`.

### 4.1 Схема базы

Схема показана в итоговом виде. Каждую таблицу создаёт миграция слайса, который её использует (раздел 16), колонки, нужные позже, добавляют миграции следующих слайсов (`ALTER TABLE`). Таблица `devices` в слайсе 1 создаётся без колонок `manual`, `label`, `notes`, `kind_locked`, `version`, `updated_by`.

```sql
CREATE TABLE devices (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	mac           TEXT NOT NULL DEFAULT '',        -- нижний регистр aa:bb:cc:dd:ee:ff, пусто если неизвестен
	ip            TEXT NOT NULL DEFAULT '',        -- у ручного устройства может быть пусто
	hostname      TEXT NOT NULL DEFAULT '',
	vendor        TEXT NOT NULL DEFAULT '',
	kind          TEXT NOT NULL DEFAULT 'unknown', -- router, switch, ap, firewall, server, nas, printer, camera, iot, host, unknown
	description   TEXT NOT NULL DEFAULT '',        -- sysDescr или uname
	source        TEXT NOT NULL,                   -- arp, snmp, ssh, local, manual
	online        INTEGER NOT NULL DEFAULT 1,      -- 0 или 1
	manual        INTEGER NOT NULL DEFAULT 0,      -- 1: создано вручную, сканы не удаляют и не помечают offline
	label         TEXT NOT NULL DEFAULT '',        -- имя от человека, показывается вместо hostname
	notes         TEXT NOT NULL DEFAULT '',
	kind_locked   INTEGER NOT NULL DEFAULT 0,      -- 1: вид задан человеком, сканы его не меняют
	version       INTEGER NOT NULL DEFAULT 1,      -- растёт при каждой правке человеком, оптимистичная блокировка
	updated_by    INTEGER REFERENCES users(id) ON DELETE SET NULL,
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	x             REAL,                            -- координаты на холсте, NULL пока не двигали
	y             REAL
);
CREATE UNIQUE INDEX devices_mac ON devices(mac) WHERE mac <> '';
CREATE UNIQUE INDEX devices_ip_nomac ON devices(ip) WHERE mac = '' AND ip <> '';

CREATE TABLE interfaces (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	name      TEXT NOT NULL,
	mac       TEXT NOT NULL DEFAULT '',
	ip        TEXT NOT NULL DEFAULT '',
	up        INTEGER NOT NULL DEFAULT 1,
	UNIQUE (device_id, name, ip)
);

CREATE TABLE links (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	a_device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	a_port       TEXT NOT NULL DEFAULT '',
	b_device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	b_port       TEXT NOT NULL DEFAULT '',
	source       TEXT NOT NULL,                    -- lldp, fdb, manual
	last_seen_at TEXT NOT NULL,
	CHECK (a_device_id < b_device_id),
	UNIQUE (a_device_id, a_port, b_device_id, b_port)
);

CREATE TABLE services (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id     INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	port          INTEGER NOT NULL,
	proto         TEXT NOT NULL,                   -- tcp, udp
	name          TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	UNIQUE (device_id, port, proto)
);

CREATE TABLE containers (
	id              TEXT PRIMARY KEY,              -- полный id Docker
	device_id       INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	name            TEXT NOT NULL,
	image           TEXT NOT NULL,
	state           TEXT NOT NULL,                 -- running, exited, paused, restarting, created, dead
	compose_project TEXT NOT NULL DEFAULT '',
	ports           TEXT NOT NULL DEFAULT '[]',    -- JSON, массив PortMapping
	networks        TEXT NOT NULL DEFAULT '[]',    -- JSON, массив ContainerNetwork
	first_seen_at   TEXT NOT NULL,
	last_seen_at    TEXT NOT NULL
);

CREATE TABLE scans (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	target          TEXT NOT NULL,                 -- цели через запятую
	status          TEXT NOT NULL,                 -- running, done, failed
	started_at      TEXT NOT NULL,
	finished_at     TEXT,
	error           TEXT NOT NULL DEFAULT '',
	device_count    INTEGER NOT NULL DEFAULT 0,
	link_count      INTEGER NOT NULL DEFAULT 0,
	container_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE changes (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	scan_id   INTEGER NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
	kind      TEXT NOT NULL,   -- device_added, device_online, device_offline, device_changed,
	                           -- link_added, link_removed, container_added, container_removed
	entity_id TEXT NOT NULL,
	summary   TEXT NOT NULL,
	at        TEXT NOT NULL
);

CREATE TABLE users (
	id                   INTEGER PRIMARY KEY AUTOINCREMENT,
	username             TEXT NOT NULL UNIQUE COLLATE NOCASE,
	display_name         TEXT NOT NULL DEFAULT '',
	password_hash        TEXT NOT NULL,            -- argon2id, формат PHC
	role                 TEXT NOT NULL,            -- viewer, operator, admin
	disabled             INTEGER NOT NULL DEFAULT 0,
	must_change_password INTEGER NOT NULL DEFAULT 0,
	failed_logins        INTEGER NOT NULL DEFAULT 0,
	locked_until         TEXT,
	created_at           TEXT NOT NULL,
	last_login_at        TEXT
);

CREATE TABLE sessions (
	id_hash      TEXT PRIMARY KEY,                 -- SHA-256 значения cookie, сам идентификатор не хранится
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	csrf_token   TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	last_seen_at TEXT NOT NULL,
	expires_at   TEXT NOT NULL,                    -- абсолютный срок жизни
	ip           TEXT NOT NULL DEFAULT '',
	user_agent   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE api_tokens (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,
	prefix       TEXT NOT NULL,                    -- первые 8 символов, для списка
	token_hash   TEXT NOT NULL UNIQUE,             -- SHA-256 токена
	scope        TEXT NOT NULL,                    -- read, write; не выше роли владельца
	created_at   TEXT NOT NULL,
	expires_at   TEXT NOT NULL,
	last_used_at TEXT,
	revoked      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audit_log (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	at        TEXT NOT NULL,
	user_id   INTEGER,                             -- NULL, если пользователь неизвестен
	username  TEXT NOT NULL DEFAULT '',
	action    TEXT NOT NULL,                       -- список в разделе 9.8
	entity    TEXT NOT NULL DEFAULT '',
	entity_id TEXT NOT NULL DEFAULT '',
	result    TEXT NOT NULL,                       -- ok, denied, error
	ip        TEXT NOT NULL DEFAULT '',
	detail    TEXT NOT NULL DEFAULT ''             -- одна строка, без секретов
);
```

Идентичность устройства: по MAC, если он известен, иначе по IP. Устройство с тем же MAC и новым IP обновляет строку, новую не создаёт. Устройство, которого нет в завершённом скане, получает `online = 0` и остаётся в базе. Обнаруженное устройство, совпавшее по IP с записью без MAC (в том числе ручной), дополняет эту запись MAC и данными скана, новую строку не создаёт. Устройства с `manual = 1` и связи с `source = 'manual'` сканы не удаляют и не помечают offline. Поля, заданные человеком (`label`, `notes`, а также `kind` при `kind_locked = 1`), сканы не перезаписывают.

### 4.2 Типы Go (`internal/model/model.go`)

```go
type DeviceKind string

const (
	KindRouter   DeviceKind = "router"
	KindSwitch   DeviceKind = "switch"
	KindAP       DeviceKind = "ap"
	KindFirewall DeviceKind = "firewall"
	KindServer   DeviceKind = "server"
	KindNAS      DeviceKind = "nas"
	KindPrinter  DeviceKind = "printer"
	KindCamera   DeviceKind = "camera"
	KindIoT      DeviceKind = "iot"
	KindHost     DeviceKind = "host"
	KindUnknown  DeviceKind = "unknown"
)

type Device struct {
	ID          int64      `json:"id"`
	MAC         string     `json:"mac"`
	IP          string     `json:"ip"`
	Hostname    string     `json:"hostname"`
	Vendor      string     `json:"vendor"`
	Kind        DeviceKind `json:"kind"`
	Description string     `json:"description"`
	Source      string     `json:"source"`
	Online      bool       `json:"online"`
	Manual      bool       `json:"manual"`
	Label       string     `json:"label"`
	Notes       string     `json:"notes"`
	KindLocked  bool       `json:"kindLocked"`
	Version     int64      `json:"version"`
	FirstSeenAt time.Time  `json:"firstSeenAt"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	X           *float64   `json:"x"`
	Y           *float64   `json:"y"`
}

type Interface struct {
	ID       int64  `json:"id"`
	DeviceID int64  `json:"deviceId"`
	Name     string `json:"name"`
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Up       bool   `json:"up"`
}

type Link struct {
	ID         int64     `json:"id"`
	ADeviceID  int64     `json:"aDeviceId"` // всегда меньше BDeviceID
	APort      string    `json:"aPort"`
	BDeviceID  int64     `json:"bDeviceId"`
	BPort      string    `json:"bPort"`
	Source     string    `json:"source"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type Service struct {
	ID          int64     `json:"id"`
	DeviceID    int64     `json:"deviceId"`
	Port        int       `json:"port"`
	Proto       string    `json:"proto"`
	Name        string    `json:"name"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

type PortMapping struct {
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Proto         string `json:"proto"`
}

type ContainerNetwork struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

type Container struct {
	ID             string             `json:"id"`
	DeviceID       int64              `json:"deviceId"`
	Name           string             `json:"name"`
	Image          string             `json:"image"`
	State          string             `json:"state"`
	ComposeProject string             `json:"composeProject"`
	Ports          []PortMapping      `json:"ports"`
	Networks       []ContainerNetwork `json:"networks"`
	FirstSeenAt    time.Time          `json:"firstSeenAt"`
	LastSeenAt     time.Time          `json:"lastSeenAt"`
}

type Scan struct {
	ID             int64      `json:"id"`
	Target         string     `json:"target"`
	Status         string     `json:"status"` // running, done, failed
	StartedAt      time.Time  `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	Error          string     `json:"error"`
	DeviceCount    int        `json:"deviceCount"`
	LinkCount      int        `json:"linkCount"`
	ContainerCount int        `json:"containerCount"`
}

type Change struct {
	ID       int64     `json:"id"`
	ScanID   int64     `json:"scanId"`
	Kind     string    `json:"kind"`
	EntityID string    `json:"entityId"`
	Summary  string    `json:"summary"`
	At       time.Time `json:"at"`
}

type Topology struct {
	Devices    []Device    `json:"devices"`
	Links      []Link      `json:"links"`
	Containers []Container `json:"containers"`
}
```

Типы пользователей и доступа (в том же пакете). Таблица «роль - права» лежит в `internal/auth/rbac.go` и повторяет раздел 9.3.

```go
type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type Permission string

const (
	PermTopologyRead Permission = "topology:read"
	PermExportRun    Permission = "export:run"
	PermTokensManage Permission = "tokens:manage"
	PermScansRun     Permission = "scans:run"
	PermDevicesWrite Permission = "devices:write"
	PermAuditRead    Permission = "audit:read"
	PermUsersManage  Permission = "users:manage"
)

type User struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	DisplayName        string     `json:"displayName"`
	Role               Role       `json:"role"`
	Disabled           bool       `json:"disabled"`
	MustChangePassword bool       `json:"mustChangePassword"`
	CreatedAt          time.Time  `json:"createdAt"`
	LastLoginAt        *time.Time `json:"lastLoginAt"`
}

// UserRecord живёт только внутри store и auth, в API не уходит.
type UserRecord struct {
	User
	PasswordHash string     `json:"-"`
	FailedLogins int        `json:"-"`
	LockedUntil  *time.Time `json:"-"`
}

type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scope      string     `json:"scope"` // read, write
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	Revoked    bool       `json:"revoked"`
}

type AuditEntry struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	UserID   *int64    `json:"userId"`
	Username string    `json:"username"`
	Action   string    `json:"action"`
	Entity   string    `json:"entity"`
	EntityID string    `json:"entityId"`
	Result   string    `json:"result"` // ok, denied, error
	IP       string    `json:"ip"`
	Detail   string    `json:"detail"`
}
```

Типы ручного ввода:

```go
type ManualDevice struct {
	MAC, IP, Hostname, Label, Notes string
	Kind                            DeviceKind
}

type DeviceEdit struct { // nil-поле не меняется
	Label   *string
	Notes   *string
	Kind    *DeviceKind // непустое значение выставляет kind_locked = 1
	Version int64       // версия, которую видел редактор, обязательное поле
}

type ManualLink struct {
	ADeviceID, BDeviceID int64
	APort, BPort         string
}
```

Типы входа коллекторов (в том же пакете). `DeviceKey` ссылается на устройство до того, как у него появился `id`: MAC приоритетнее IP.

```go
type DeviceKey struct {
	MAC string
	IP  string
}

type DeviceInput struct {
	Key         DeviceKey
	Hostname    string
	Vendor      string
	Description string
	Kind        DeviceKind
	Source      string
}

type InterfaceInput struct {
	Device DeviceKey
	Name   string
	MAC    string
	IP     string
	Up     bool
}

type LinkInput struct {
	A, B         DeviceKey
	APort, BPort string
	Source       string
}

type ServiceInput struct {
	Device DeviceKey
	Port   int
	Proto  string
	Name   string
}

type ContainerInput struct {
	Device         DeviceKey
	ID, Name       string
	Image, State   string
	ComposeProject string
	Ports          []PortMapping
	Networks       []ContainerNetwork
}
```

Полей `owner`, `location`, `vlan` у устройства в v0 нет. Понадобились - `CONTRACT GAP`.

### 4.3 Конфиг (YAML)

Тип `config.Secret` заменяет значение на `***` при выводе через `String()`, `MarshalJSON` и `slog`.

```yaml
listen: 127.0.0.1:8080
database: ./netdoc.db
allowPublicTargets: false

scan:
  targets: [192.168.1.0/24]
  interval: 15m                  # 0 отключает расписание
  ports: [22, 53, 80, 443, 445, 3389, 8080]   # пустой список отключает скан портов

snmp:
  communities: [public]          # Secret
  timeout: 2s

ssh:
  knownHosts: ~/.ssh/known_hosts
  hosts:
    - address: 192.168.1.10
      user: audit
      keyFile: ~/.ssh/netdoc_ed25519    # только ключи, паролей нет

docker:
  enabled: true
  socket: /var/run/docker.sock

history:
  keepScans: 200

audit:
  keepDays: 365

auth:
  sessionIdleTimeout: 8h         # от 15m до 24h
  sessionMaxAge: 168h            # не меньше sessionIdleTimeout, не больше 720h
  minPasswordLength: 12          # не меньше 12
  maxFailedLogins: 5             # от 3 до 20
  lockoutDuration: 15m
  argon2:
    memoryKiB: 65536             # не меньше 19456
    iterations: 3                # не меньше 2
    parallelism: 2               # не меньше 1

tls:
  certFile: ""                   # certFile и keyFile задаются вместе или не задаются
  keyFile: ""

trustedProxies: []               # CIDR обратных прокси, чьи X-Forwarded-* принимаются

discovery:
  routerTables: true             # ARP и FDB по SNMP с известных маршрутизаторов и коммутаторов
  probeRoutedTargets: false      # ICMP и TCP-зондирование подсетей за маршрутизатором

export:
  chromePath: ""                 # пусто: искать chromium и google-chrome в PATH
```

---

## 5. Контракты доступа к данным и коллекторов (черновик)

### 5.1 `store.Repo`

`internal/store/store.go` экспортирует ровно это:

```go
type Repo interface {
	ListDevices(ctx context.Context, f DeviceFilter) ([]model.Device, error)
	GetDevice(ctx context.Context, id int64) (model.Device, error)            // ErrNotFound
	SetDevicePosition(ctx context.Context, id int64, x, y float64) error      // ErrNotFound
	ListInterfaces(ctx context.Context, deviceID int64) ([]model.Interface, error)
	ListLinks(ctx context.Context) ([]model.Link, error)
	ListServices(ctx context.Context, deviceID int64) ([]model.Service, error) // 0 значит все устройства
	ListContainers(ctx context.Context) ([]model.Container, error)
	Topology(ctx context.Context) (model.Topology, error)                      // одна транзакция чтения

	CreateDevice(ctx context.Context, in model.ManualDevice, userID int64) (model.Device, error)         // ErrConflict
	UpdateDevice(ctx context.Context, id int64, e model.DeviceEdit, userID int64) (model.Device, error)  // ErrNotFound, ErrVersionConflict
	DeleteDevice(ctx context.Context, id int64) error                                                     // ErrNotFound
	CreateLink(ctx context.Context, in model.ManualLink) (model.Link, error)                             // ErrNotFound, ErrConflict
	DeleteLink(ctx context.Context, id int64) error                                                       // ErrNotFound, ErrNotManual

	StartScan(ctx context.Context, target string) (model.Scan, error)
	FinishScan(ctx context.Context, id int64, status, errMsg string) error
	ListScans(ctx context.Context, limit int) ([]model.Scan, error)
	GetScan(ctx context.Context, id int64) (model.Scan, error)                 // ErrNotFound

	ApplyResult(ctx context.Context, scanID int64, r collector.Result) error
	ListChanges(ctx context.Context, limit int) ([]model.Change, error)
	PruneScans(ctx context.Context, keep int) error
}

type DeviceFilter struct {
	Online *bool
	Kind   model.DeviceKind
	Query  string
}
```

`AuthRepo` (тот же пакет) хранит пользователей, сессии, токены и аудит:

```go
type AuthRepo interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, u model.UserRecord) (model.User, error)         // ErrConflict
	GetUserByName(ctx context.Context, username string) (model.UserRecord, error)   // ErrNotFound
	GetUser(ctx context.Context, id int64) (model.User, error)                      // ErrNotFound
	ListUsers(ctx context.Context) ([]model.User, error)
	UpdateUser(ctx context.Context, id int64, role *model.Role, disabled *bool) (model.User, error) // ErrNotFound, ErrLastAdmin
	SetPassword(ctx context.Context, id int64, hash string, mustChange bool) error  // отзывает сессии пользователя
	RecordLogin(ctx context.Context, id int64, ok bool, lockAfter int, lockFor time.Duration) error
	DeleteUser(ctx context.Context, id int64) error                                 // ErrNotFound, ErrLastAdmin

	CreateSession(ctx context.Context, s SessionRecord) error
	GetSession(ctx context.Context, idHash string) (SessionRecord, model.User, error) // ErrNotFound
	TouchSession(ctx context.Context, idHash string, now time.Time) error
	DeleteSession(ctx context.Context, idHash string) error
	DeleteUserSessions(ctx context.Context, userID int64) error
	PruneSessions(ctx context.Context, now time.Time) error

	CreateToken(ctx context.Context, t TokenRecord) (model.APIToken, error)
	GetTokenByHash(ctx context.Context, hash string) (TokenRecord, model.User, error) // ErrNotFound
	ListTokens(ctx context.Context, userID int64) ([]model.APIToken, error)
	RevokeToken(ctx context.Context, id, userID int64) error                         // ErrNotFound
	TouchToken(ctx context.Context, id int64, now time.Time) error

	AddAudit(ctx context.Context, e model.AuditEntry) error
	ListAudit(ctx context.Context, f AuditFilter) ([]model.AuditEntry, error)
	PruneAudit(ctx context.Context, keepDays int) error
}
```

`SessionRecord`, `TokenRecord` и `AuditFilter` объявлены в `store` и содержат колонки соответствующих таблиц (раздел 4.1).

Правила:

- Первый аргумент всегда `context.Context`. Ошибка «не найдено» - это `store.ErrNotFound`, а не `nil` и не паника. Остальные ошибки пакета: `ErrConflict` (нарушение уникальности), `ErrVersionConflict`, `ErrNotManual` (попытка удалить связь, найденную сканом), `ErrLastAdmin`.
- `Open(path string)` включает `journal_mode = WAL`, `foreign_keys = ON`, `busy_timeout = 5000`. Для `":memory:"` ставит `SetMaxOpenConns(1)`.
- `ApplyResult` работает в одной транзакции: слияние устройств по идентичности, upsert интерфейсов, связей, сервисов и контейнеров, пометка отсутствующих (`online = 0`, удаление исчезнувших связей, сервисов и контейнеров), запись в `changes`, обновление счётчиков скана.
- `ApplyResult` не затрагивает устройства с `manual = 1` и связи с `source = 'manual'` (не помечает offline, не удаляет). Исчезнувшие связи удаляются только с источником `lldp` и `fdb`.
- `ApplyResult` вызывается только для скана, в котором прошёл этап обнаружения. Неудавшийся скан не меняет состояние и не помечает устройства offline.
- `Query` обрезается по краям. Пустая строка после обрезки означает, что фильтра нет. Совпадение ищется подстрокой по `ip`, `mac`, `hostname`, `vendor` без учёта регистра. Отбор идёт в Go: `LIKE` в SQLite знает регистр только для ASCII.
- Сортировка по IP выполняется в Go, по числовому значению адреса.

### 5.2 Коллекторы

`internal/collector/collector.go`:

```go
type Input struct {
	Targets []netip.Prefix
	Devices []model.Device // живые устройства после этапа обнаружения
	Config  config.Config
}

type Result struct {
	Devices    []model.DeviceInput
	Interfaces []model.InterfaceInput
	Links      []model.LinkInput
	Services   []model.ServiceInput
	Containers []model.ContainerInput
}

type Collector interface {
	Name() string
	Collect(ctx context.Context, in Input) (Result, error)
}
```

Правила:

- Порядок в `scanner`: `arp` (обнаружение в L2-сегменте) → `probe` (маршрутизируемые подсети, если включено) → `snmparp` (ARP и FDB маршрутизаторов и коммутаторов) → обратный DNS → `snmp` → `lldp` → `services` → `classify` → `ssh` → `docker`. `classify` не делает сетевых запросов: он выставляет `Kind` по вендору, данным SNMP, LLDP-возможностям и открытым портам.
- Ошибка коллектора после этапа обнаружения не валит скан: ошибка пишется в лог, остальные коллекторы работают, статус скана `done`. Ошибка обнаружения даёт статус `failed`.
- Слияние результатов нескольких коллекторов по `DeviceKey`: непустое значение побеждает пустое. При конфликте побеждает источник по приоритету `ssh`, `snmp`, `arp`. `Kind = unknown` никогда не затирает известный вид.
- Каждый коллектор получает зависимости интерфейсом (`snmpClient`, `dialer`, `sshRunner`, `dockerAPI`), чтобы тесты подставляли фейки.
- Каждая горутина завершается по отмене `ctx`. Утечек горутин нет.

---

## 6. UI-компоненты

Лежат в `web/src/components/`, используются во всех слайсах. Свои кнопки, поля и таблицы в страницах не писать.

Примитивы shadcn/ui подключаются командой CLI в том слайсе, который их использует: `button`, `card`, `table`, `badge`, `tabs`, `sheet`, `dropdown-menu`, `input`, `skeleton`, `sonner`, `tooltip`, `dialog`, `alert-dialog`, `select`, `textarea`, `label`, `alert`.

Собственные компоненты:

| компонент         | пропсы                                                              |
| ----------------- | ------------------------------------------------------------------- |
| `StatusBadge`     | `online: boolean`                                                   |
| `EmptyState`      | `title`, `text`, `action?` (слот)                                   |
| `ScanBar`         | `scan: Scan или null`, `onStart()`, `progress?`                     |
| `DeviceNode`      | узел React Flow, `data: { device, containerCount }`                 |
| `TopologyCanvas`  | `topology: Topology`, `onMove(id, x, y)`, `onSelect(id или null)`   |
| `DevicePanel`     | `deviceId: number`, `onClose()`                                     |
| `ExportMenu`      | `formats: ('md', 'html', 'pdf', 'drawio')[]`                        |
| `DeviceForm`      | `device?: Device` (пусто: создание), `onSubmit(values)`, `onCancel()` |
| `RoleBadge`       | `role: Role`                                                        |
| `UserMenu`        | `user: User`, `onLogout()`                                          |
| `RequirePermission` | `permission: Permission`, `fallback?`, дети                       |

Отдельного роута-витрины нет. Компонент, который не используется ни одним слайсом, не пишется.

---

## 7. Маршруты и API

### 7.1 HTTP API

| метод и путь                         | что делает                                                        | право           | коды ответа            |
| ------------------------------------ | ----------------------------------------------------------------- | --------------- | ---------------------- |
| `GET /healthz`                       | проверка живости, без данных                                      | публичный       | 200                    |
| `POST /api/auth/login`               | тело `{ "username", "password" }`, ставит cookie сессии           | публичный       | 200, 400, 401, 429     |
| `POST /api/auth/logout`              | завершает сессию                                                  | только вход     | 204                    |
| `GET /api/auth/me`                   | текущий пользователь, его права, CSRF-токен                       | только вход     | 200, 401               |
| `PUT /api/auth/password`             | смена своего пароля (текущий и новый)                             | только вход     | 204, 400, 401          |
| `GET /api/topology`                  | устройства, связи, контейнеры одним ответом                       | `topology:read` | 200                    |
| `GET /api/devices?online=&kind=&q=`  | список устройств                                                  | `topology:read` | 200, 400               |
| `POST /api/devices`                  | ручное добавление устройства                                      | `devices:write` | 201, 400, 409          |
| `GET /api/devices/{id}`              | устройство, его интерфейсы и сервисы                              | `topology:read` | 200, 404               |
| `PATCH /api/devices/{id}`            | правка `label`, `notes`, `kind`, поле `version` обязательно       | `devices:write` | 200, 400, 404, 409     |
| `DELETE /api/devices/{id}`           | удаление устройства                                               | `devices:write` | 204, 404               |
| `PUT /api/devices/{id}/position`     | тело `{ "x": 1, "y": 2 }`, сохраняет координаты                   | `devices:write` | 204, 400, 404          |
| `POST /api/links`                    | ручная связь между устройствами                                   | `devices:write` | 201, 400, 404, 409     |
| `DELETE /api/links/{id}`             | удаление ручной связи                                             | `devices:write` | 204, 404, 409          |
| `GET /api/containers`                | список контейнеров                                                | `topology:read` | 200                    |
| `GET /api/services?deviceId=`        | список сервисов                                                   | `topology:read` | 200, 400               |
| `GET /api/scans`                     | последние сканы                                                   | `topology:read` | 200                    |
| `POST /api/scans`                    | запускает скан                                                    | `scans:run`     | 202, 409               |
| `GET /api/scans/{id}`                | один скан                                                         | `topology:read` | 200, 404               |
| `GET /api/changes?limit=`            | лента изменений, новые сверху                                     | `topology:read` | 200, 400               |
| `GET /api/events`                    | поток SSE                                                         | `topology:read` | 200                    |
| `GET /api/export/{format}`           | `md`, `html`, `pdf`, `drawio`, файл с `Content-Disposition`       | `export:run`    | 200, 404, 503          |
| `GET /api/users`                     | список пользователей                                              | `users:manage`  | 200                    |
| `POST /api/users`                    | создание пользователя (имя, роль, временный пароль)               | `users:manage`  | 201, 400, 409          |
| `PATCH /api/users/{id}`              | смена роли, отключение                                            | `users:manage`  | 200, 400, 404, 409     |
| `PUT /api/users/{id}/password`       | задать новый пароль, включает `must_change_password`              | `users:manage`  | 204, 400, 404          |
| `DELETE /api/users/{id}`             | удаление пользователя                                             | `users:manage`  | 204, 404, 409          |
| `GET /api/tokens`                    | свои токены доступа                                               | `tokens:manage` | 200                    |
| `POST /api/tokens`                   | выпуск токена, значение возвращается один раз                     | `tokens:manage` | 201, 400, 409          |
| `DELETE /api/tokens/{id}`            | отзыв токена                                                      | `tokens:manage` | 204, 404               |
| `GET /api/audit?limit=&before=`      | журнал аудита с фильтрами                                         | `audit:read`    | 200, 400               |

События SSE: `scan.started`, `scan.progress` (данные `{ scanId, stage, done, total }`), `scan.finished`, `topology.changed`, `config.reloaded`, `config.error`. События `config.*` получают только пользователи с правом `users:manage`.

### 7.2 Страницы интерфейса

| путь            | что показывает                                                  |
| --------------- | --------------------------------------------------------------- |
| `/`             | топология, боковая панель устройства по `?device=<id>`          |
| `/inventory`    | таблица устройств и IP, поиск, фильтры                          |
| `/containers`   | таблица контейнеров                                             |
| `/services`     | таблица сервисов                                                |
| `/changes`      | лента изменений и график числа устройств по сканам              |
| `/login`        | вход                                                            |
| `/account`      | смена пароля, токены доступа                                    |
| `/users`        | управление пользователями (право `users:manage`)                |
| `/audit`        | журнал аудита (право `audit:read`)                              |

Правила:

- Ответы API в JSON, поля `camelCase`. Ошибка: `{ "error": "text" }` с подходящим кодом.
- Нечисловой `id` (`/api/devices/abc`) даёт 404, а не 500 и не 400.
- Одновременно идёт один скан. `POST /api/scans` во время скана отвечает 409.
- Тело запроса ограничено 1 МиБ.
- Все пути вне `/api` и `/healthz` отдают встроенный `index.html`, роутинг делает React Router.
- Экспорт `pdf` без найденного Chrome отвечает 503 с текстом, как получить PDF через печать HTML.
- Любой маршрут кроме `/healthz` и `POST /api/auth/login` требует сессии (cookie) или токена (`Authorization: Bearer`). Нет аутентификации - 401, нет права - 403.
- Небезопасные методы (`POST`, `PUT`, `PATCH`, `DELETE`) с сессией требуют заголовок `X-CSRF-Token`, токену он не нужен.
- Токен доступа не вызывает `/api/users`, `/api/audit` и `/api/auth/*` (кроме `GET /api/auth/me`).
- Тексты ответов 401 и 403 не раскрывают, существует ли пользователь или ресурс.
- Колонка «право» таблицы 7.1 и есть таблица маршрутов из `internal/api/router.go`. Маршрут без права не регистрируется.

---

## 8. Валидация

`config.Validate() error` (возвращает `errors.Join`) и проверки входа API. Чистые функции без сети и базы.

Правила конфига:

- Каждая цель в `scan.targets` - корректный CIDR.
- Цель - IPv4. IPv6 в версии 1 не поддерживается, потому что обнаружение идёт по ARP.
- Префикс IPv4-цели не шире `/16`.
- Цель лежит в диапазонах RFC 1918, если `allowPublicTargets` не включён.
- `scan.interval` равен `0` или не меньше `1m`.
- `snmp.timeout` от `1s` до `30s`.
- `ssh.hosts[].keyFile` обязателен для каждого хоста.
- `auth.sessionIdleTimeout` от `15m` до `24h`, `auth.sessionMaxAge` не меньше простоя и не больше `720h`.
- `auth.minPasswordLength` не меньше 12, `auth.maxFailedLogins` от 3 до 20.
- Параметры `auth.argon2` не ниже `memoryKiB 19456`, `iterations 2`, `parallelism 1`.
- `tls.certFile` и `tls.keyFile` задаются вместе или не задаются.
- Каждый элемент `trustedProxies` - корректный CIDR.
- `listen` не на loopback без `tls` и без `trustedProxies` → предупреждение в лог, что трафик идёт без шифрования. Запуск не блокируется.

Правила API:

- `x` и `y` позиции - конечные числа по модулю не больше 100000.
- `limit` - целое от 1 до 500, по умолчанию 50.
- Неизвестный `format` экспорта - 404.
- Имя пользователя `^[a-z0-9._-]{3,32}$`, роль из набора `viewer`, `operator`, `admin`.
- Пароль от `auth.minPasswordLength` до 128 символов и не равен имени пользователя.
- Ручное устройство: `ip` пустой или корректный адрес, `mac` пустой или корректный (приводится к нижнему регистру с двоеточиями), хотя бы одно из полей `ip`, `mac`, `label` не пусто, `label` до 100 символов, `notes` до 2000, `kind` из набора `DeviceKind`.
- Связь: оба устройства существуют и различны, порт до 64 символов.
- Токен: имя от 1 до 64 символов, область `read` или `write`, срок от 1 до 365 дней.

Сообщения об ошибках на английском, по одной строке: `target is not a valid CIDR`, `target is wider than /16`, `target is outside private ranges`, `scan interval is below 1m`, `snmp timeout is out of range`, `ssh host has no key file`, `position is not finite`, `limit is out of range`, `username is invalid`, `password is shorter than the minimum`, `password is longer than 128 characters`, `password equals username`, `role is unknown`, `ip is not valid`, `mac is not valid`, `device has no ip, mac or label`, `label is longer than 100 characters`, `notes are longer than 2000 characters`, `link connects a device to itself`, `session timeout is out of range`, `argon2 parameters are below the minimum`, `tls needs both certFile and keyFile`, `token lifetime is out of range`, `only IPv4 targets are supported`, `min password length is below 12`, `max failed logins is out of range`, `trusted proxy is not a valid CIDR`.

---

## 9. Безопасность (руководство, обязательно для всех слайсов)

Принцип: запрет по умолчанию, минимальные права, сервис только читает сеть. Все параметры ниже заданы заранее. Параметр меняется только через `CONTRACT GAP` (раздел 14). Слайс не ослабляет параметр ради удобства разработки.

### 9.1 Модель угроз

Что защищаем:

- карту сети и инвентарь (они раскрывают топологию и слабые места);
- учётные данные доступа к устройствам (SNMP community, ключи SSH);
- учётные записи, сессии и токены панели;
- возможность запускать сканы из сети хоста;
- доступ к Docker-сокету хоста.

От кого защищаем:

- сетевой посетитель без учётной записи;
- пользователь с низкой ролью, который пытается получить права выше;
- содержимое из сети: hostname, `sysDescr` и имена контейнеров контролируют третьи лица;
- похищенные сессия или токен.

Вне защиты v1: злоумышленник с доступом к файловой системе хоста (он читает конфиг и базу) и атаки на само сетевое оборудование.

| угроза                    | мера                                                                                                   | где проверяется                 |
| ------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------- |
| подбор пароля             | argon2id, блокировка учётки, лимит попыток с IP, одинаковый ответ на любую ошибку входа                | слайс 2                         |
| кража или фиксация сессии | случайный идентификатор, хеш в базе, `HttpOnly`, `SameSite=Strict`, новый идентификатор при входе, отзыв сессий | слайсы 2 и 6           |
| CSRF                      | `SameSite=Strict` и заголовок `X-CSRF-Token` для небезопасных методов                                  | слайс 2                         |
| XSS через данные сети     | экранирование при выводе, CSP без inline-скриптов, запрет `dangerouslySetInnerHTML`                    | каждый слайс, который выводит данные |
| повышение прав            | таблица маршрутов с правом, проверка на сервере, запрет по умолчанию                                   | слайсы 2, 3, 5, 6               |
| утечка секретов           | тип `Secret`, фильтр логов, секреты только в конфиге                                                   | слайсы 0, 7, 10                 |
| злоупотребление сканером  | цели из конфига, RFC 1918 по умолчанию, лимиты параллелизма, право `scans:run`, аудит                  | слайсы 1 и 3                    |
| перегрузка сервиса        | лимиты тела и заголовков, таймауты, лимиты SSE и PDF                                                   | слайсы 3 и 13                   |
| доступ через Docker-сокет | сокет только на чтение, рекомендуется `docker-socket-proxy`                                            | слайсы 11 и 16                  |
| уязвимые зависимости      | `govulncheck`, `npm audit`, Dependabot                                                                 | слайс 16                        |

### 9.2 Аутентификация

| параметр                  | значение                                                                                                      |
| ------------------------- | ------------------------------------------------------------------------------------------------------------- |
| хеш пароля                | argon2id, память 64 MiB, 3 прохода, параллелизм 2, соль 16 байт, ключ 32 байта, формат PHC. Нижняя граница (OWASP): 19 MiB, 2 прохода, параллелизм 1 |
| перехеширование           | при успешном входе, если параметры в хеше слабее текущих                                                       |
| длина пароля              | от 12 до 128 символов, любые символы Unicode, без правил состава, не равен имени пользователя                 |
| имя пользователя          | `^[a-z0-9._-]{3,32}$`, регистр не различается                                                                  |
| ответ на ошибку входа     | всегда 401 и текст `invalid username or password`: неверный пароль, неизвестный пользователь, заблокированная или отключённая учётка |
| выравнивание времени      | для неизвестного пользователя выполняется проверка по фиктивному хешу                                          |
| блокировка учётки         | 5 неудачных входов подряд, блокировка на 15 минут, успешный вход сбрасывает счётчик                            |
| лимит по IP               | 10 попыток входа в минуту с одного IP, ответ 429 с заголовком `Retry-After`                                    |
| идентификатор сессии      | 32 случайных байта из `crypto/rand`, base64url, в базе только SHA-256                                          |
| cookie сессии             | `netdoc_session`, `HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` при HTTPS                                  |
| срок сессии               | 8 часов без активности, 7 дней абсолютно, новый идентификатор при каждом входе                                 |
| лимит сессий              | 10 на пользователя, самая старая вытесняется                                                                   |
| отзыв сессий              | при выходе, смене пароля, смене роли, отключении и удалении пользователя                                       |
| CSRF-токен                | 32 случайных байта на сессию, отдаётся в `GET /api/auth/me`, передаётся в `X-CSRF-Token`                       |
| токен доступа             | `ndt_` и 32 случайных байта base64url, в базе SHA-256 и префикс из 8 символов, значение показывается один раз  |
| область токена            | `read` (права viewer) или `write` (права operator), не выше роли владельца, никогда не даёт `users:manage` и `audit:read` |
| срок токена               | по умолчанию 90 дней, максимум 365, бессрочных нет, не больше 20 активных на пользователя                      |

Первый запуск и служебные правила:

- Пароля по умолчанию нет. Пока в базе нет пользователей, все маршруты кроме `/healthz` отвечают 503 с текстом `no users: run "netdoc user add"`.
- `netdoc user add <name> --role admin` читает пароль с терминала без эха или из stdin (`--password-stdin`). Аргумента с паролем нет.
- Учётная запись с `must_change_password` может только сменить пароль.
- Последнего администратора нельзя удалить, отключить или понизить.
- Отключить аутентификацию нельзя.

### 9.3 Роли и права

| право           | что разрешает                                                                              | viewer | operator | admin |
| --------------- | ------------------------------------------------------------------------------------------ | ------ | -------- | ----- |
| `topology:read` | чтение топологии, инвентаря, сервисов, контейнеров, изменений, поток событий                | да     | да       | да    |
| `export:run`    | экспорт документации                                                                       | да     | да       | да    |
| `tokens:manage` | выпуск и отзыв собственных токенов                                                         | да     | да       | да    |
| `scans:run`     | запуск скана                                                                               | нет    | да       | да    |
| `devices:write` | ручное добавление, правка и удаление устройств и ручных связей, позиции узлов на холсте    | нет    | да       | да    |
| `audit:read`    | чтение журнала аудита                                                                      | нет    | нет      | да    |
| `users:manage`  | управление пользователями, события `config.*`                                              | нет    | нет      | да    |

Правила:

- Права проверяет только middleware по таблице маршрутов. Обработчики роль не сравнивают.
- Маршрут без права в таблице не регистрируется: регистрация паникует при старте. Запрет по умолчанию.
- Маршруты `/healthz` и `POST /api/auth/login` публичные, остальные требуют сессии или токена. Маршруты `/api/auth/*` кроме входа требуют только входа, без права.
- Пользователь не меняет собственную роль и не отключает сам себя.
- Интерфейс скрывает недоступные действия по `web/src/lib/permissions.ts`. Это удобство, защита работает на сервере.
- Объектных прав нет: все пользователи видят всю сеть (раздел 18).

Многопользовательская работа:

- Топология общая. Позиции узлов: побеждает последняя запись.
- Правки устройств идут с оптимистичной блокировкой по `version`. Расхождение даёт 409, интерфейс предлагает загрузить актуальные данные.
- Каждое значимое действие пишется в журнал аудита (9.8).

### 9.4 Транспорт и заголовки

- Панель слушает `127.0.0.1:8080` по умолчанию. Адрес вне loopback без TLS и без доверенного прокси даёт предупреждение в лог и баннер администраторам в интерфейсе.
- TLS: встроенный (`tls.certFile`, `tls.keyFile`, минимум TLS 1.2) или обратный прокси. Заголовки `X-Forwarded-For` и `X-Forwarded-Proto` принимаются только от адресов из `trustedProxies`, иначе игнорируются.
- `Strict-Transport-Security: max-age=31536000` отправляется только по HTTPS.
- Заголовки на всех ответах:
  - `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: no-referrer`
  - `X-Frame-Options: DENY`
  - `Permissions-Policy: camera=(), microphone=(), geolocation=()`
  - `Cross-Origin-Opener-Policy: same-origin`
  - `Cache-Control: no-store` для всех путей `/api`
- Inline-скриптов нет. Скрипты подключаются только внешними файлами.
- CORS не включается: только same-origin.
- Экспорт HTML отдаётся как вложение (`Content-Disposition: attachment`) с собственным `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; img-src data:` и `X-Content-Type-Options: nosniff`.

### 9.5 Ввод, вывод и ресурсы

- Данные из сети (hostname, `sysName`, `sysDescr`, имена контейнеров, образы) недоверенные. При записи в базу они обрезаются до 255 символов, управляющие символы удаляются. Выводятся только через экранирующие механизмы: React, `html/template`, XML-энкодер. `dangerouslySetInnerHTML` запрещён правилом eslint.
- Запросы к базе только параметризованные (sqlc). Сборки SQL из строк нет.
- Тело запроса не больше 1 МиБ, заголовки не больше 16 КиБ.
- Таймауты сервера: `ReadHeaderTimeout` 10 с, `ReadTimeout` 30 с, `WriteTimeout` 60 с, `IdleTimeout` 120 с. SSE и экспорт PDF исключены из `WriteTimeout`, у них свои дедлайны.
- SSE: не больше 5 соединений на пользователя. PDF: один рендер одновременно, не дольше 60 секунд.
- Пути файлов из пользовательского ввода не принимаются. Имена экспортируемых файлов формирует сервер.

### 9.6 Сканирование

- Цели только из конфига. По умолчанию только RFC 1918, не шире `/16`. Публичные адреса включает флаг `allowPublicTargets`.
- Пределы параллелизма: 128 отправок ARP-прогрева, 256 TCP-соединений за скан, 32 одновременных SNMP-запроса, 4 SSH-хоста. Одновременно идёт один скан.
- Запуск скана требует права `scans:run`. Минимальный интервал расписания 1 минута.
- Ручное добавление устройства ничего не зондирует.
- Зондирование маршрутизируемых подсетей (`discovery.probeRoutedTargets`) по умолчанию выключено.
- SNMP: в v1 только v2c, community передаётся открытым текстом. README требует read-only community и ACL на устройстве. В клиенте нет метода записи, SNMP SET невозможен.
- SSH: только ключи, `known_hosts` проверяется строго, команды фиксированы, отдельная учётная запись без прав записи. Права файла ключа не шире `0600`, иначе хост пропускается.
- Docker: сокет подключается только на чтение. Рекомендуется `docker-socket-proxy` с разрешением только `containers`, `networks`, `events`. Риск доступа к сокету описан в README.

### 9.7 Секреты и данные на диске

- Секреты (`snmp.communities`, пути к ключам SSH) живут только в конфиге. Права файла конфига не шире `0600`, иначе сервис отказывается запускаться. Флаг `--allow-loose-permissions` разрешает запуск с предупреждением для окружений вроде bind-mount в Docker.
- Файл базы создаётся с правами `0600`, каталог данных с `0700`. Шифрования базы в v1 нет, защита на уровне ОС (раздел 18).
- Хеши паролей, токенов и сессий не возвращаются API, не логируются и не попадают в экспорт.
- В логах (`slog`) запрещены пароли, токены, значения cookie, заголовок `Authorization` и community. Фильтр покрыт тестом.
- Экспорт не содержит секретов, учётных данных и данных пользователей.

### 9.8 Аудит

Пишутся события: `login`, `login_failed`, `logout`, `password_change`, `user_create`, `user_update`, `user_delete`, `user_password_reset`, `token_create`, `token_revoke`, `scan_start`, `device_create`, `device_edit`, `device_delete`, `link_create`, `link_delete`, `export`, `config_reload`, `config_reload_failed`, `access_denied`.

Запись содержит время, пользователя, IP, результат (`ok`, `denied`, `error`) и одну строку описания без секретов. API не умеет менять и удалять записи: старые записи удаляет только очистка по `audit.keepDays` (365 по умолчанию). Журнал читает только право `audit:read`.

### 9.9 Зависимости и поставка

- `govulncheck ./...` и `npm audit --omit=dev --audit-level=high` запускаются командой `make audit` в CI. В локальный `make gate` они не входят, потому что требуют сеть.
- Версии зафиксированы (`go.sum`, `package-lock.json`). Dependabot следит за `go.mod`, npm и базовым образом Docker.
- Docker-образ: минимальный итоговый образ, `read_only: true`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`. Процесс работает не от root, где это возможно (доступ к сокету решается через `group_add`).
- Релизы публикуются с файлом контрольных сумм (`checksums.txt`).
- В репозитории лежит `SECURITY.md` с порядком сообщения об уязвимостях.

---

## 10. Тесты

Тесты выводятся из критериев приёмки слайса, а не из готового кода. Критерии написаны в разделе 16 до того, как написан код. Тест кодирует критерий и не повторяет реализацию.

Запрещённый вид теста: «записали устройство, прочитали, значения совпали». Такой тест зелёный и на сломанном слиянии по MAC, и на дублях.

На каждый слайс минимум:

- тест на каждый критерий приёмки, где есть проверяемое условие;
- тест на путь ошибки (цель вне разрешённых диапазонов, таймаут SNMP, отсутствующий Docker-сокет, несуществующий id);
- тесты чистой логики (парсеры, раскладка, валидация, сортировка IP) без сети и без базы.

Как строить тесты:

- База: `store.Open(":memory:")`, новая на каждый тест. Файл `netdoc.db` тесты не трогают.
- Сеть: коллекторы принимают интерфейсы (`snmpClient`, `dialer`, `sshRunner`, `dockerAPI`), тесты подставляют фейки. Ответы SNMP берутся из записанного вывода `snmpwalk` в `testdata/`.
- HTTP: `httptest` поверх `api.NewRouter` с базой в памяти и фейковым сканером.
- Время: пакеты принимают `clock func() time.Time`, тесты подставляют фиксированное.
- Рендеры: golden-файлы в `testdata/*.golden` плюс структурные проверки (XML разбирается, число ячеек `mxCell` равно числу устройств, контейнеров и связей плюс две служебные). Golden обновляется флагом `-update` и проверяется глазами на ревью.
- Права: один тест обходит всю таблицу маршрутов и проверяет для каждого маршрута три случая: без входа - 401, роль без права - 403, роль с правом - ни 401, ни 403. Регистрация маршрута без права паникует, это тоже проверяется.
- Безопасность: тесты на заголовки (на `/`, `/api/*`, ошибках и экспорте), на атрибуты cookie сессии, на то, что пароли, хеши, токены и community не попадают в логи, ответы API и экспорт.
- Интеграционные тесты против `deploy/compose.lab.yaml` лежат под build tag `lab` и в гейт не входят.
- Фронтенд: vitest на чистую логику из `web/src/lib/`. Компоненты проверяются руками по критериям слайса.

---

## 11. Проверки и гейт

`Makefile`:

```
make fmt     # gofmt -w, prettier --write
make lint    # gofmt -l (вывод пустой), go vet, golangci-lint, sqlc diff, eslint, prettier --check
make check   # tsc --noEmit
make test    # go test -race ./..., vitest run
make gate    # lint, check, test
make audit   # govulncheck и npm audit, нужна сеть, запускается в CI, в gate не входит
make gen     # sqlc generate
make dev     # air (бэкенд) и vite (фронтенд) параллельно
make build   # сборка фронтенда, затем go build в один бинарник
```

`make gate` красный → слайс не закрыт. Правки по гейту делаются в том же заходе.

Что ловит каждая команда:

- gofmt и prettier - расхождения форматирования;
- go vet и golangci-lint (включая gosec) - неиспользуемый код, неотловленные ошибки, подозрительные конструкции, небезопасные вызовы;
- sqlc diff - сгенерированный код отстал от SQL;
- eslint - мёртвые импорты, хуки React с неверными зависимостями, a11y-ошибки, `dangerouslySetInnerHTML`;
- govulncheck и npm audit (в CI, командой `make audit`) - известные уязвимости зависимостей;
- tsc - типы, в том числе расхождение с типами раздела 4;
- go test -race - критерии приёмки и гонки;
- vitest - чистая логика фронтенда.

---

## 12. Конвенция текста

Код, комментарии, коммиты, логи, тексты интерфейса, сообщения валидации, README - по-английски. Этот файл на время черновика на русском, финальная версия переводится на английский.

Коммиты, Conventional Commits, фиксированный формат:

```
type(scope): summary
```

- `type` из набора `feat|fix|test|refactor|chore|docs`;
- `scope` из набора `config|store|collector|render|api|web|deploy`;
- `summary` в императиве, со строчной буквы, без точки, до 50 символов;
- тело только чтобы объяснить *почему*, не *что*;
- коммитить по ходу работы маленькими шагами, не одним коммитом в конце. Каждый коммит по возможности проходит `make check`.

Примеры: `feat(collector): add arp warmup sweep`, `test(store): cover mac identity merge`.

Правила письма для всей прозы проекта (коммиты, комментарии, README): активный залог, конкретика вместо общих фраз, без вводных оборотов, без длинного тире, без наречий-усилителей. Комментарий объясняет причину решения, а не пересказывает соседнюю строку. Закомментированный код не оставлять.
Перед написанием README посмотри [https://docs.github.com/en](https://docs.github.com/en), [https://github.com/matiassingers/awesome-readme](https://github.com/matiassingers/awesome-readme), [https://www.makeareadme.com/](https://www.makeareadme.com/)

---

## 13. Definition of Done одного слайса

1. Критерии приёмки слайса из раздела 16 выполнены, проверены руками (в браузере для интерфейса, в терминале для CLI).
2. Тесты написаны из критериев, `make gate` зелёный.
3. Новых файлов и абстракций сверх описанных в этом файле нет.
4. Мёртвого кода нет: неиспользуемых экспортов, компонентов, стилей, SQL-запросов.
5. Сгенерированный код sqlc актуален (`sqlc diff` пуст).
6. Коммиты по конвенции раздела 12.
7. Каждый новый маршрут объявлен в таблице маршрутов с правом, параметры раздела 9, которых касается слайс, выполнены и проверены тестами.
8. `tech.md` не изменён (изменение контракта идёт отдельно, через раздел 14).

---

## 14. CONTRACT GAP

Не хватает поля, типа, функции, маршрута или опции конфига - работа останавливается. Выдай блок и жди ответа:

```
CONTRACT GAP
Что нужно: <поле/тип/функция/маршрут>
Зачем: <какой критерий приёмки без него не выполняется>
Предлагаемая форма: <точная сигнатура, колонка с типом или поле JSON>
Что делаю пока: <заглушка локально в своём слайсе / жду>
```

Код с выдуманным типом не пиши. Схему базы, типы `model`, интерфейсы `Repo`, `AuthRepo` и `Collector`, HTTP-контракт, формат конфига, матрицу прав и параметры раздела 9 сам не расширяй и не ослабляй.

---

## 15. Правила поведения в сессии

- Думай до кода: назови допущения, спроси при неоднозначности, покажи варианты вместо молчаливого выбора.
- Простота: никаких фич сверх запрошенного, никаких абстракций под одноразовый код, никакой обработки ошибок, которых не бывает.
- Хирургические правки: соседний рабочий код не улучшать и не рефакторить. Каждая изменённая строка следует из текущей задачи.
- Один слайс за заход. Не выкатывай всё приложение сразу.
- Сетевой код не запускай против чужих сетей. Все проверки идут против лаборатории (`deploy/compose.lab.yaml`), фикстур или сети владельца проекта.
- Параметры безопасности из раздела 9 не ослабляй ради удобства. Для разработки `cmd/seed --dev-user` создаёт учётку `dev` с ролью `admin`, пароль берётся из переменной `NETDOC_DEV_PASSWORD`, пароля по умолчанию нет.
- Ревью идёт вторым заходом, после того как слайс готов, а не в том же сообщении, где написан код.

---

## 16. Стадии и слайсы

Порядок жёсткий, сверху вниз. Один слайс за один заход сессии. Слайс закрыт, когда выполнен Definition of Done из раздела 13.

Слайс вертикальный: от таблицы в базе или коллектора до экрана или файла экспорта. Половина слайса не закрывается.

- **Стадия 1 - каркас.** Слайс 0. Фичи не начинаются, пока критерии каркаса не зелёные целиком.
- **Стадия 2 - обнаружение, доступ и первая панель.** Слайсы 1-4: ARP-обнаружение и CLI, аутентификация и роли, API с таблицей устройств, топология на холсте. Аутентификация идёт до API: ни один маршрут не появляется без проверки прав.
- **Стадия 3 - ручное управление и пользователи.** Слайсы 5-6: ручное добавление и правка устройств, управление пользователями, токены и журнал аудита.
- **Стадия 4 - обогащение.** Слайсы 7-11: SNMP и LLDP, расширенное обнаружение, сервисы, SSH, Docker-контейнеры.
- **Стадия 5 - документы.** Слайсы 12-13: Markdown и Draw.io, HTML и PDF.
- **Стадия 6 - эксплуатация.** Слайсы 14-15: история изменений, расписание и горячая перезагрузка конфига.
- **Стадия 7 - выпуск.** Слайсы 16-17: упаковка и Docker, полировка интерфейса.

---

### Слайс 0 - каркас

**Что собрать:**

- Go-модуль `netdoc`, `Makefile` с целями из раздела 11, `.air.toml`, `.golangci.yml`, `sqlc.yaml`, `.gitignore` (`netdoc.db*`, `web/dist`, `web/node_modules`, `tmp`).
- `internal/model/model.go` - типы и константы из раздела 4.2.
- `internal/config` - загрузка YAML, `Validate` по разделу 8, тип `Secret`, `netdoc.example.yaml`.
- `internal/store/store.go` - `Open(path)`, `ErrNotFound`, подключение `goose`. Миграций таблиц пока нет.
- `internal/api/router.go` - таблица маршрутов `route(method, path, permission, handler)`: `GET /healthz` помечен публичным, маршрут без права не регистрируется. Раздача встроенного фронтенда с откатом на `index.html`.
- `cmd/netdoc/main.go` - подкоманда `serve` с флагом `--config`.
- `web/` - Vite + React + TypeScript strict + Tailwind + shadcn/ui (инициализация), React Router, `web/embed.go`.
- Каркас интерфейса: боковая панель с названием и ссылками на существующие страницы, страница `/` с `EmptyState` «No scans yet».
- Прокси Vite на `/api` и `/healthz` в бэкенд для разработки.

**Критерии приёмки:**

1. `make gate` зелёный на пустом проекте.
2. `make dev`: правка `.go` файла перезапускает сервер (air), правка `.tsx` обновляет страницу без перезагрузки (HMR). Проверено руками.
3. `make build` даёт один бинарник. `./netdoc serve` открывает `/` без ошибок в консоли, `GET /healthz` отвечает 200.
4. `store.Open(":memory:")` даёт рабочую базу без файла на диске.
5. `netdoc.example.yaml` проходит `Validate`.

**Тесты:** `internal/config/config_test.go` - валидный конфиг, цель не CIDR, цель шире `/16`, публичная цель без флага, публичная цель с флагом, интервал меньше `1m`, интервал `0`, таймаут SNMP вне диапазона, хост SSH без ключа, `Secret` скрывает значение в `String()`, JSON и `slog`. `internal/store/store_test.go` - `Open(":memory:")` работает. `internal/api/router_test.go` - регистрация маршрута без права паникует, `/healthz` отвечает 200 без входа.

**Не делать:** коллекторы, страницы фич, аутентификацию, Dockerfile, тему оформления.

---

### Слайс 1 - ARP-обнаружение и CLI

**Файлы:** `internal/store/migrations/0001_devices_scans.sql` (таблицы `devices` и `scans`, `devices` без колонок ручного ввода, раздел 4.1), `internal/store/queries/*.sql`, `internal/store/apply.go`, `internal/collector/collector.go`, `arp.go`, `scanner.go`, `cmd/netdoc/main.go` (подкоманды `scan`, `devices`).

**Что делает:** `netdoc scan` берёт цели из конфига, шлёт UDP-пакет на порт 9 каждого адреса цели с ограничением в 128 одновременных отправок (ядро выполняет ARP-запрос перед отправкой, права root не нужны), ждёт 2 секунды и читает `/proc/net/arp`. Записи с флагом `0x2` считаются живыми. Для каждого живого адреса выполняется обратный DNS с таймаутом 1 секунда. `netdoc devices` печатает таблицу: IP, MAC, hostname, online. Обнаружение работает для адресов того же L2-сегмента, что и хост netdoc.

**Критерии приёмки:**

1. `netdoc scan` на сети /24 сохраняет найденные устройства, `netdoc devices` показывает их.
2. Повторный скан не создаёт дублей. Устройство с тем же MAC и новым IP обновляет строку.
3. Устройство, которого нет в следующем завершённом скане, получает `online = false` и остаётся в базе.
4. `netdoc scan 8.8.8.8/32` без `allowPublicTargets` завершается ошибкой `target is outside private ranges`.
5. Ctrl+C останавливает скан не дольше чем за 2 секунды, статус скана `failed`, ошибка `canceled`, состояние устройств не изменилось.
6. Hostname из обратного DNS попадает в таблицу, недоступный DNS не замедляет скан дольше таймаута.

**Тесты:** парсер `/proc/net/arp` на фикстуре (полные и неполные записи), `ApplyResult` на базе в памяти (слияние по MAC, смена IP, пометка offline, отмена не меняет состояние), `scanner` с фейковым коллектором (отмена по `ctx`, ошибка обнаружения даёт `failed`), проверка цели из аргумента.

**Не делать:** ICMP, SNMP, скан портов, HTTP API, определение вида устройства, адреса вне L2-сегмента.

---

### Слайс 2 - аутентификация, роли и сессии

**Файлы:** `internal/store/migrations/NNNN_auth.sql` (таблицы `users`, `sessions`, `audit_log`), `internal/auth/` (`password.go`, `session.go`, `rbac.go`, `ratelimit.go`), реализация `AuthRepo` в `internal/store/`, `internal/api/router.go` (проверка права), `middleware.go`, `auth.go`, `cmd/netdoc/main.go` (подкоманда `user`), `web/src/pages/Login.tsx`, `Account.tsx`, `web/src/components/UserMenu.tsx`, `RequirePermission.tsx`, `web/src/lib/permissions.ts`, `web/src/api/client.ts` (заголовок CSRF, обработка 401).

**Что делает:**

- Подкоманды `netdoc user add <name> --role <role>`, `user list`, `user disable <name>`, `user passwd <name>`. Пароль читается с терминала без эха или из stdin.
- API `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/me`, `PUT /api/auth/password`.
- Цепочка middleware в таком порядке: заголовки безопасности, лимиты тела и таймауты, аутентификация (сессия, токены доступа добавляет слайс 6), CSRF, проверка права, обработчик, аудит.
- Проверка права по таблице маршрутов из слайса 0 включается в middleware: маршрут с правом без входа даёт 401, при недостатке прав 403.
- Интерфейс: страница `/login`, переход на `/login` при ответе 401, `UserMenu` с выходом, `/account` со сменой пароля, `RequirePermission`.
- Все параметры берутся из раздела 9.

**Критерии приёмки:**

1. Пока пользователей нет, все маршруты кроме `/healthz` отвечают 503 с текстом `no users: run "netdoc user add"`.
2. `netdoc user add` создаёт пользователя. Пароль не принимается аргументом командной строки. В базе лежит хеш argon2id в формате PHC.
3. Вход с верными данными отвечает 200 и ставит cookie `netdoc_session` с `HttpOnly` и `SameSite=Strict`. Неверный пароль, неизвестный пользователь и заблокированная учётка дают одинаковый ответ 401 `invalid username or password`.
4. Пятая неудачная попытка подряд блокирует учётку на 15 минут (вход с верным паролем в это время тоже даёт 401), успешный вход сбрасывает счётчик. Больше 10 попыток в минуту с одного IP дают 429 с `Retry-After`.
5. Запрос без сессии к защищённому маршруту даёт 401, с неверным или истёкшим идентификатором тоже.
6. Небезопасный метод с сессией и без верного `X-CSRF-Token` даёт 403.
7. Сессия истекает через 8 часов без активности и через 7 дней абсолютно. Выход и смена пароля отзывают сессии пользователя.
8. Все ответы (страницы, `/api`, ошибки) содержат заголовки из раздела 9.4.
9. Пользователь с `must_change_password` может только сменить пароль.
10. Последнего администратора нельзя отключить, в том числе командой CLI.
11. Вход, неудачный вход, выход и отказ в доступе (403) пишутся в `audit_log`.
12. Значения паролей и хешей не попадают в логи и ответы.

**Тесты:** `password` (хеш и проверка, параметры, перехеширование), жизненный цикл сессии с подставляемыми часами (простой, абсолютный срок, новый идентификатор при входе), `ratelimit`, цепочка middleware через `httptest`, полнота таблицы маршрутов (см. раздел 10), заголовки безопасности, CSRF, запись в аудит, отсутствие секретов в логах.

**Не делать:** OIDC и LDAP, двухфакторную аутентификацию, самостоятельную регистрацию, восстановление пароля по почте, «запомнить меня», роли сверх трёх.

---

### Слайс 3 - API, SSE и таблица устройств

**Файлы:** `internal/api/router.go`, `devices.go`, `scans.go`, `sse.go`, `internal/events/hub.go`, `cmd/netdoc/main.go` (`serve` запускает сканер), `web/src/api/client.ts` (дополняется), `events.ts`, `web/src/pages/Inventory.tsx`, `web/src/lib/` (сортировка IP), компоненты `ScanBar`, `StatusBadge`, `EmptyState`, примитивы `table`, `badge`, `button`, `input`, `skeleton`.

**Что делает:** маршруты `GET /api/devices`, `GET /api/devices/{id}`, `GET /api/scans`, `POST /api/scans`, `GET /api/scans/{id}`, `GET /api/events`. Все маршруты объявлены в таблице с правами `topology:read` (чтение и события) и `scans:run` (`POST /api/scans`). Кнопка запуска скана видна только при праве `scans:run`. Страница `/inventory` показывает таблицу устройств с поиском и фильтром online. `ScanBar` запускает скан и показывает прогресс из SSE. Таблица обновляется по событию `topology.changed`.

**Критерии приёмки:**

1. `POST /api/scans` отвечает 202 и запускает скан. Второй `POST` во время скана отвечает 409.
2. `/inventory` показывает устройства из базы. После завершения скана таблица обновляется без перезагрузки страницы.
3. Поиск находит устройство по IP, MAC и hostname без учёта регистра, включая кириллические имена.
4. Сортировка по IP числовая: `192.168.1.9` идёт выше `192.168.1.10`.
5. `/api/devices/abc` и `/api/devices/999` отвечают 404.
6. Пустая база показывает `EmptyState`, для роли с правом `scans:run` с кнопкой запуска первого скана.
7. Запрос без входа к любому маршруту этого слайса даёт 401. Роль `viewer` получает данные (200), но на `POST /api/scans` получает 403 и не видит кнопку запуска. Роли `operator` и `admin` запускают скан.
8. Поток `GET /api/events` без входа даёт 401. События скана доходят только до вошедших пользователей.

**Тесты:** обработчики через `httptest` на базе в памяти (критерии 1, 3, 5, 7, 8), `hub` (подписка, публикация, отписка без утечки горутины), `sortByIp` в vitest (критерий 4).

**Не делать:** пагинацию, WebSocket, экспорт, график.

---

### Слайс 4 - топология на холсте

**Файлы:** `internal/render/layout.go`, `internal/api/topology.go`, `cmd/seed/main.go`, `web/src/pages/Topology.tsx`, компоненты `TopologyCanvas`, `DeviceNode`, `DevicePanel`, примитивы `sheet`, `tooltip`.

**Что делает:** `GET /api/topology` отдаёт устройства, связи и контейнеры (связи и контейнеры пока пустые). Координаты в ответе заполнены всегда: сохранённые `x`, `y` остаются как есть, у остальных устройств их считает `render.Layout`. Раскладка по слоям: ряды по виду (router, firewall, switch, ap, server, nas, printer, camera, iot, host, unknown), внутри ряда порядок по IP, шаг 220 на 140, ряд переносится после 8 узлов. Раскладка детерминирована. Страница `/` рисует узлы на React Flow с зумом, панорамой и мини-картой. Перетаскивание сохраняет позицию через `PUT /api/devices/{id}/position` (право `devices:write`). Для роли без этого права узлы не перетаскиваются. Клик по узлу открывает `DevicePanel`. `cmd/seed` создаёт демо-сеть (роутер, 2 коммутатора, 20 хостов, 4 сервера) и дополняется в слайсах 7 и 11. Флаг `--dev-user` создаёт учётную запись `dev` с ролью `admin` и паролем из `NETDOC_DEV_PASSWORD`.

**Критерии приёмки:**

1. `/` рисует все устройства узлами с именем, IP, иконкой вида и состоянием online или offline.
2. Перетащенный узел остаётся на месте после перезагрузки страницы.
3. Новое устройство после скана появляется на холсте, остальные узлы не сдвигаются.
4. Клик по узлу открывает панель с данными устройства, закрытие и `Esc` её скрывают. `/?device=<id>` открывает панель при загрузке.
5. На базе из 200 демо-устройств холст остаётся отзывчивым при панораме и перетаскивании (проверка руками).
6. `PUT` с нечисловыми или бесконечными координатами отвечает 400, с несуществующим id отвечает 404. Роль `viewer` получает 403, узлы для неё не перетаскиваются.

**Тесты:** `layout_test.go` (детерминизм, отсутствие совпадающих позиций, сохранённые координаты не меняются), обработчик `PUT` (критерий 6).

**Не делать:** кнопку авто-раскладки, группировку узлов, маршрутизацию рёбер, ручное создание связей.

---

### Слайс 5 - ручное добавление и правка устройств и связей

**Файлы:** `internal/store/migrations/NNNN_device_edits.sql` (колонки `manual`, `label`, `notes`, `kind_locked`, `version`, `updated_by`), методы `Repo` (`CreateDevice`, `UpdateDevice`, `DeleteDevice`, `CreateLink`, `DeleteLink`), `internal/store/apply.go` (правила слияния), `internal/api/devices.go` (`POST`, `PATCH`, `DELETE`), `internal/api/links.go`, `web/src/components/DeviceForm.tsx`, режим правки в `DevicePanel`, диалог добавления на `/inventory` и на холсте, примитивы `dialog`, `select`, `textarea`, `alert-dialog`.

**Что делает:** оператор добавляет устройства, которые сеть не показывает (неуправляемый коммутатор, патч-панель, устройство на другой площадке), правит `label`, `notes` и вид (вид фиксируется флагом `kind_locked`), удаляет устройства и ручные связи, соединяет узлы на холсте перетаскиванием с вводом имён портов. Ручное устройство без IP и без MAC допускается. Ручные данные переживают сканы. Ручное устройство, совпавшее с обнаруженным по MAC или IP, сливается с ним в одну запись (правило идентичности из раздела 4.1).

**Критерии приёмки:**

1. Оператор создаёт устройство с `label` и видом без IP и MAC. Оно появляется в списке и на холсте и помечено как ручное.
2. Скан не помечает ручное устройство offline и не удаляет его.
3. Устройство, созданное вручную с IP, и устройство, обнаруженное сканом с тем же IP, объединяются в одну строку: ручные поля остаются, MAC и данные скана добавляются.
4. Правка `label`, `notes` и вида переживает повторный скан. Вид с `kind_locked` скан не меняет.
5. `PATCH` с устаревшим `version` отвечает 409 и не затирает чужую правку. Интерфейс предлагает загрузить актуальные данные.
6. Ручная связь создаётся перетаскиванием между узлами, скан её не удаляет, оператор удаляет её вручную. Удаление связи, найденной сканом, отвечает 409.
7. Роль `viewer` не видит кнопок правки, а прямые запросы `POST`, `PATCH`, `DELETE` от неё отвечают 403.
8. Создание, правка и удаление пишутся в аудит.
9. Недопустимые значения (некорректные IP и MAC, слишком длинные поля) отклоняются с 400 и сообщениями из раздела 8.

**Тесты:** слияние ручного и найденного устройства (3), ручные устройства не уходят в offline (2), `kind_locked` (4), конфликт версий (5), права по ролям (7), валидация (9), записи аудита (8).

**Не делать:** импорт CSV, группы и теги, пользовательские поля, историю правок.

---

### Слайс 6 - пользователи, токены доступа, журнал аудита

**Файлы:** `internal/store/migrations/NNNN_api_tokens.sql`, методы `AuthRepo` для администрирования пользователей, токенов и аудита, `internal/auth/session.go` (токены), `internal/api/users.go`, `tokens.go`, `audit.go`, `web/src/pages/Users.tsx`, `Audit.tsx`, `Account.tsx` (раздел токенов), `web/src/components/RoleBadge.tsx`.

**Что делает:** администратор создаёт, отключает и удаляет пользователей, меняет роли и задаёт временный пароль (`must_change_password`). Каждый пользователь выпускает и отзывает собственные токены доступа для скриптов (`Authorization: Bearer`). Администратор читает журнал аудита с фильтрами по пользователю, действию, результату и периоду. `PruneAudit` вызывается при старте и раз в сутки.

**Критерии приёмки:**

1. Администратор создаёт пользователя с ролью, пользователь входит и видит ровно то, что разрешает его роль (viewer, operator, admin).
2. Смена роли, отключение, удаление и сброс пароля отзывают активные сессии пользователя.
3. Последнего администратора нельзя удалить, отключить или понизить (409), в том числе самому себе.
4. Operator и viewer не открывают `/users` и `/audit` и получают 403 на соответствующие маршруты.
5. Токен показывается один раз при создании, в списке виден только префикс, в базе лежит только хеш. Токен с областью `read` получает 403 на небезопасных методах. Токен никогда не открывает `users:manage` и `audit:read`.
6. Отозванный и просроченный токены дают 401. Срок по умолчанию 90 дней, максимум 365, бессрочный токен не выдаётся.
7. Журнал показывает события из раздела 9.8 с фильтрами и пагинацией через `before`. Через API записи нельзя изменить или удалить.
8. Не больше 20 активных токенов и 10 сессий на пользователя.
9. Записи старше `audit.keepDays` удаляются.

**Тесты:** матрица ролей на тестовых данных, отзыв сессий, последний администратор, токены (хеш, область, срок, отзыв), фильтры и пагинация аудита, `PruneAudit`.

**Не делать:** группы пользователей, пользовательские роли, права на уровне устройств, сброс пароля по почте, единый вход (SSO).

---

### Слайс 7 - SNMP, интерфейсы и LLDP-связи

**Файлы:** `internal/store/migrations/NNNN_interfaces_links.sql`, `internal/collector/snmp.go`, `lldp.go`, `oui.go`, `oui.csv`, обновление `scanner.go`, `cmd/seed`, `web/src/components/DeviceNode.tsx`, `TopologyCanvas.tsx`, `DevicePanel.tsx`.

**Что делает:** для каждого живого устройства коллектор пробует SNMP v2c с community из конфига и читает `sysName`, `sysDescr`, `sysServices`, таблицы интерфейсов и адресов. LLDP-коллектор читает `lldpLocPortTable` и `lldpRemTable` и строит связи между известными устройствами: удалённый узел сопоставляется по MAC из chassis id или по адресу управления. Неизвестный сосед создаёт устройство с источником `snmp`. Вид устройства определяется по LLDP-возможностям (bridge → switch, router → router, WLAN access point → ap), иначе остаётся прежним. Вендор берётся из встроенной таблицы OUI по MAC. Связи рисуются рёбрами с подписями портов.

**Критерии приёмки:**

1. Устройство, ответившее по SNMP, получает `hostname = sysName`, `description = sysDescr`, источник `snmp`. Неответившее остаётся как есть, скан не падает.
2. Интерфейсы сохраняются по устройству, повторный скан не создаёт дублей.
3. Сосед по LLDP у коммутатора A, являющийся коммутатором B, даёт одну связь A.port - B.port, одна строка, а не две.
4. Связь с неизвестным соседом создаёт устройство-соседа.
5. Связь, пропавшая из LLDP-таблицы, удаляется после следующего завершённого скана.
6. Рёбра рисуются на холсте с подписями портов, вкладка интерфейсов в `DevicePanel` показывает имя, MAC, IP и состояние.
7. Вендор по OUI виден в панели устройства.
8. Значения community не появляются в логах, ответах API и выводе CLI.

**Тесты:** `snmpClient` с фейком на фикстурах `snmpwalk` (критерии 1, 2), нормализация и дедупликация связи (`a_device_id < b_device_id`, критерий 3), создание соседа (4), удаление пропавшей связи (5), определение вида по LLDP-возможностям, `Secret` в логах (8).

**Не делать:** SNMP v3, SNMP SET, счётчики трафика, приём trap, загрузку MIB-файлов (OID задаются числами).

---

### Слайс 8 - расширенное обнаружение и классификация

**Файлы:** `internal/collector/snmparp.go`, `probe.go`, `classify.go`, `classify_rules.csv`, обновление `scanner.go`, секция `discovery` конфига, `cmd/seed`.

**Границы обнаружения.** Ни один сканер не находит любое устройство. Устройство, которое молчит и не передаёт трафик, видно только как запись, добавленная вручную (слайс 5).

**Что делает:**

- `snmparp`: у известных маршрутизаторов и коммутаторов, ответивших по SNMP, читает ARP-таблицу (`ipNetToMediaTable`) и таблицу пересылки (`dot1dTpFdbTable`, `dot1qTpFdbTable`). Так находятся устройства из других сегментов и устройства, которые не отвечают на ARP-прогрев, но недавно передавали трафик. Для хоста из FDB создаётся связь с портом коммутатора (источник `fdb`). Порты, у которых есть LLDP-сосед (магистральные), связей `fdb` не создают.
- `probe` (только при `discovery.probeRoutedTargets: true`): для целей вне L2-сегмента отправляет ICMP echo через непривилегированный ICMP-сокет (нужен `net.ipv4.ping_group_range`) и делает TCP connect на порты 22, 80, 443. Живым считается адрес, ответивший на любую пробу, включая RST. Не больше 128 одновременных проб.
- `classify`: выставляет вид устройства по правилам в порядке приоритета: LLDP-возможности, SNMP (`sysServices`, ключевые слова `sysDescr`), открытые порты (9100, 515, 631 дают `printer`, 554 даёт `camera`), вендор по OUI. Правила лежат в данных (`classify_rules.csv`), а не в коде. Без совпадений остаётся `unknown`. Вид с `kind_locked` не меняется.

**Критерии приёмки:**

1. Устройство другого сегмента, известное ARP-таблице маршрутизатора, появляется в инвентаре с источником `snmp`.
2. Устройство, не ответившее на ARP-прогрев, но присутствующее в FDB коммутатора, появляется в инвентаре.
3. Хост из FDB получает связь с портом коммутатора. Магистральные порты связей не создают.
4. При `probeRoutedTargets: false` цели вне сегмента не зондируются, в лог пишется причина. При `true` живые адреса находятся ICMP- или TCP-пробой.
5. Устройство с открытым портом 9100 получает вид `printer`, с портом 554 вид `camera`. Вид с `kind_locked` не меняется.
6. Правила читаются из `classify_rules.csv`. Устройство без совпадений остаётся `unknown`.

**Тесты:** фикстуры SNMP для ARP и FDB (критерии 1-3), исключение магистральных портов, `probe` с фейковыми ICMP и `dialer`, таблица правил `classify` на тестовом CSV, `kind_locked`.

**Не делать:** DHCP-аренды, mDNS, SSDP и NetBIOS (раздел 18), сканирование за NAT, SYN-скан на raw-сокетах, определение ОС по отпечатку стека.

---

### Слайс 9 - сервисы

**Файлы:** `internal/store/migrations/NNNN_services.sql`, `internal/collector/services.go`, `web/src/pages/Services.tsx`, вкладка сервисов в `DevicePanel`.

**Что делает:** для каждого живого устройства коллектор делает TCP connect-скан портов из `scan.ports`: таймаут 500 мс на порт, не больше 256 одновременных соединений за скан. Имя сервиса берётся из встроенной таблицы известных портов (IANA). Страница `/services` показывает сервисы всех устройств с фильтром по имени сервиса.

**Критерии приёмки:**

1. Открытый порт сохраняется как сервис с именем, закрывшийся порт удаляется после следующего завершённого скана.
2. Скан берёт порты из `scan.ports`. Пустой список отключает этап.
3. Одновременных соединений за скан не больше 256.
4. `/services` показывает имя, порт, протокол и устройство. Фильтр по имени работает.
5. Вкладка сервисов в `DevicePanel` показывает сервисы выбранного устройства.

**Тесты:** фейковый `dialer` (критерии 1-3, счётчик одновременных соединений), таблица имён портов, фильтр на фронтенде (vitest).

**Не делать:** чтение баннеров, определение версий, UDP-скан, проверку уязвимостей, интеграцию с nmap.

---

### Слайс 10 - SSH-коллектор

**Файлы:** `internal/collector/ssh.go`, обновление `scanner.go`, секция `ssh` конфига.

**Что делает:** для хостов из `ssh.hosts` коллектор подключается по ключу и выполняет фиксированный набор команд на чтение: `hostname`, `uname -sr`, `ip -o addr`, `ss -H -tln`. По выводу заполняются `hostname`, `description`, интерфейсы с IP и слушающие сервисы. Ключ хоста проверяется по файлу `ssh.knownHosts`. Каждая команда выполняется с таймаутом 10 секунд, вывод ограничен 1 МиБ, подключение с таймаутом 5 секунд.

**Критерии приёмки:**

1. Хост из конфига дополняется: hostname, описание (`uname`), интерфейсы с IP, слушающие сервисы. Источник `ssh`.
2. Неизвестный или изменившийся ключ хоста: хост пропускается, скан продолжается, предупреждение попадает в лог.
3. Недоступный хост пропускается после таймаута, скан не падает.
4. Коллектор выполняет только команды из константного списка. Команд из конфига или из ввода пользователя нет.
5. Пути к ключам и содержимое ключей не попадают в логи, API и экспорт.
6. Файл ключа с правами шире `0600` отклоняется: хост пропускается с предупреждением.

**Тесты:** фейковый `sshRunner` на фикстурах (критерии 1, 3), парсеры `ip -o addr` и `ss -H -tln`, политика ключей хоста на `known_hosts` в памяти (критерий 2), тест на то, что выполняются только команды из списка (критерий 4).

**Не делать:** `sudo` и команды от root, интерактивные сессии, вход по паролю, Windows-хосты, разбор CLI сетевого оборудования, бэкап конфигураций.

---

### Слайс 11 - Docker-контейнеры

**Файлы:** `internal/store/migrations/NNNN_containers.sql`, `internal/collector/docker.go`, `internal/api/containers.go`, `web/src/pages/Containers.tsx`, обновление `TopologyCanvas`, `DeviceNode`, `layout.go`, `cmd/seed`.

**Что делает:** коллектор подключается к сокету из `docker.socket`, читает все контейнеры (любые состояния), их порты, сети и метку `com.docker.compose.project`. Хостом контейнеров служит устройство, чей IP совпадает с локальным адресом netdoc. Если его нет в таблице, коллектор создаёт его с источником `local`. Подписка на события Docker (`create`, `start`, `stop`, `die`, `destroy`) обновляет базу между сканами и публикует `topology.changed`. На холсте контейнеры стоят под узлом хоста (по 6 в ряд), от хоста к контейнеру идёт ребро, цвет зависит от состояния. Режим только чтение: запуск, остановка и логи не реализуются. Данные о контейнерах отдаются по праву `topology:read`.

**Критерии приёмки:**

1. При доступном сокете `/containers` показывает имя, образ, состояние, порты, сети и compose-проект каждого контейнера.
2. Запуск или остановка контейнера обновляет страницу не позже чем через 2 секунды без скана.
3. Сокет недоступен (нет файла или нет прав): сервис работает, страница показывает `EmptyState` с причиной, ошибка в логе записана один раз.
4. Контейнеры на холсте стоят под узлом хоста, клик открывает панель контейнера.
5. Удалённый контейнер исчезает из списка и с холста.
6. `docker.enabled: false` полностью отключает коллектор.

**Тесты:** фейковый `dockerAPI` на фикстурах (критерии 1, 5), обработчик событий (2), отсутствие сокета (3), маппинг портов и сетей в JSON, раскладка контейнеров под хостом.

**Не делать:** Podman и LXD, удалённые Docker-хосты по TCP, `exec`, логи, управление контейнерами.

---

### Слайс 12 - экспорт Markdown и Draw.io

**Файлы:** `internal/render/mermaid.go`, `markdown.go`, `drawio.go`, `internal/api/export.go`, `cmd/netdoc/main.go` (подкоманда `export`), `web/src/components/ExportMenu.tsx`, примитив `dropdown-menu`.

**Что делает:** Markdown содержит заголовок, дату генерации, сводку по количеству, топологию в блоке Mermaid (`graph LR`), таблицы устройств (имя, IP, MAC, вид, вендор, состояние), IP по интерфейсам, сервисов и контейнеров. Draw.io - несжатый XML mxGraph: по вершине на устройство и контейнер с координатами из `render.Layout`, по ребру на связь с подписями портов, цвет по виду. `netdoc export --format md|drawio --out <файл>` и `GET /api/export/{format}` дают один и тот же результат. CLI читает локальную базу напрямую, права проверяет ОС (доступ к файлу базы).

**Критерии приёмки:**

1. `GET /api/export/md` отдаёт Markdown с `Content-Disposition` вида `netdoc-<дата>.md`. Блок Mermaid отображается на GitHub (проверка руками).
2. `GET /api/export/drawio` отдаёт XML, который открывается в diagrams.net без ошибок, позиции совпадают с панелью.
3. Одно и то же состояние базы даёт одинаковые байты, кроме строки с датой генерации (она берётся из подставляемых часов).
4. Пустая база даёт документ с пометкой «no devices», а не ошибку.
5. Символы `<`, `&`, `"`, `|` в hostname экранируются в XML, в таблицах Markdown и в метках Mermaid.
6. `netdoc export` и API дают одинаковый результат.
7. `GET /api/export/{format}` без права `export:run` отвечает 403, успешный экспорт пишется в аудит. Файл отдаётся с `Content-Disposition: attachment`.

**Тесты:** golden-файлы на небольшой фикстуре, разбор XML и подсчёт ячеек, тесты экранирования (критерий 5), детерминизм (3), пустая база (4).

**Не делать:** HTML и PDF, пользовательские шаблоны, библиотеки иконок оборудования, слои Draw.io.

---

### Слайс 13 - экспорт HTML и PDF

**Файлы:** `internal/render/svg.go`, `html.go`, `pdf.go`, `templates/report.html.tmpl`, обновление `export.go`, `ExportMenu.tsx`.

**Что делает:** HTML - один самодостаточный файл: встроенные стили, встроенная SVG-диаграмма (те же координаты, что на панели), таблицы устройств, сервисов, контейнеров и изменений, стили для печати (`@media print`, разрывы страниц). Скриптов и внешних запросов нет. PDF создаётся через `chromedp`, который печатает тот же HTML. Chrome или Chromium ищется по `export.chromePath` или в `PATH`.

**Критерии приёмки:**

1. HTML открывается офлайн, не делает сетевых запросов и не содержит `<script>`.
2. SVG показывает те же позиции узлов, что и панель, offline-устройства приглушены.
3. В предпросмотре печати диаграмма умещается по ширине, строки таблиц не разрываются между страницами (проверка руками).
4. `GET /api/export/pdf` отдаёт валидный PDF (заголовок `%PDF`) при наличии Chrome. Без Chrome отвечает 503 с понятным текстом.
5. Hostname вида `<script>` выводится как текст.
6. `viewBox` SVG вычисляется по границам узлов.
7. HTML отдаётся как вложение с заголовками из раздела 9.4 (включая собственный `Content-Security-Policy`). PDF-рендер выполняется не более одного за раз и не дольше 60 секунд.

**Тесты:** HTML без `<script` (1), экранирование (5), границы `viewBox` (6), заголовки и лимит PDF-рендера (7), PDF с пропуском через `t.Skip` без Chrome и ветка 503 с фейковым поиском браузера (4).

**Не делать:** темы отчёта, логотип и брендинг, графики в отчёте, шифрование PDF.

---

### Слайс 14 - история сканов и изменения

**Файлы:** `internal/store/migrations/NNNN_changes.sql`, `internal/store/apply.go` (запись `changes`, `PruneScans`), `internal/api/changes.go`, `web/src/pages/Changes.tsx`, секция «Changes» в экспортах.

**Что делает:** `ApplyResult` сравнивает результат скана с предыдущим состоянием и пишет изменения видов из раздела 4.1. Первый завершённый скан изменений не пишет. Страница `/changes` показывает ленту (новые сверху) и линейный график числа устройств по завершённым сканам (Recharts). `history.keepScans` ограничивает число хранимых сканов, старые удаляются вместе с изменениями. Экспорты получают секцию «Changes» по последнему скану.

**Критерии приёмки:**

1. Скан, который добавил, вернул, потерял или изменил устройство, связь или контейнер, пишет изменения с видами из контракта. Скан без отличий не пишет ничего.
2. Первый завершённый скан не пишет изменений.
3. Неудавшийся скан не пишет изменений, не помечает устройства offline и не попадает на график.
4. `/changes` показывает ленту и график. Пустая лента показывает `EmptyState`.
5. Лишние сканы сверх `history.keepScans` удаляются вместе со своими изменениями.
6. Экспорты Markdown и HTML содержат секцию «Changes» с изменениями последнего скана.

**Тесты:** по одному тесту на каждый вид изменения на базе в памяти, первый скан без изменений, неудавшийся скан, `PruneScans`.

**Не делать:** уведомления (email, webhook), сравнение произвольных сканов, откат состояния.

---

### Слайс 15 - расписание и горячая перезагрузка конфига

**Файлы:** `internal/config/watch.go`, `internal/collector/scanner.go` (планировщик), `internal/events/hub.go` (события `config.*`), тост в `web/src/main.tsx`.

**Что делает:** планировщик запускает скан каждые `scan.interval`. Файл конфига отслеживается через `fsnotify` (наблюдение за каталогом, чтобы ловить атомарную замену файла редактором, задержка 200 мс) и по `SIGHUP`. Корректный конфиг подменяется атомарно, публикуется `config.reloaded`. Некорректный конфиг отбрасывается, работает прежний, публикуется `config.error` с текстом ошибки. Применяются на лету: `scan.*`, `snmp.*`, `ssh.*`, `docker.*`, `history.*`, `export.*`. Поля `listen` и `database` требуют перезапуска, в лог пишется предупреждение.

**Критерии приёмки:**

1. Смена `scan.interval` в файле применяется со следующего тика без перезапуска.
2. Смена целей применяется к следующему скану, идущий скан завершается со старыми настройками.
3. Битый YAML или ошибка валидации оставляют прежний конфиг, интерфейс показывает тост с текстом ошибки, сервис работает.
4. Сохранение файла через замену (vim, `mv`) подхватывается.
5. `SIGHUP` вызывает перезагрузку.
6. Планировщик не запускает второй скан во время идущего, тик пропускается с записью в лог.
7. Смена `docker.socket` или `docker.enabled` переподключает Docker-коллектор.
8. События `config.reloaded` и `config.error` получают только пользователи с правом `users:manage`, остальные их не видят. Текст ошибки не содержит значений секретов. Перезагрузка и ошибка перезагрузки пишутся в аудит.

**Тесты:** планировщик с подставляемыми часами (1, 6), перезагрузка корректного и некорректного конфига (2, 3), замена файла во временном каталоге (4), переподключение Docker-коллектора с фейком (7), доставка событий по ролям (8).

**Не делать:** веб-редактор конфига, расписание по устройствам, cron-выражения, перезагрузку `listen` на лету.

---

### Слайс 16 - упаковка и Docker

**Файлы:** `deploy/Dockerfile`, `deploy/compose.yaml`, `deploy/compose.lab.yaml`, `deploy/lab/`, `.goreleaser.yaml`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `README.md`, `LICENSE`, `SECURITY.md`, `.github/dependabot.yml`, интеграционный тест под build tag `lab`.

**Что делает:**

- `Dockerfile`: многоступенчатая сборка (фронтенд, статический бинарник Go), минимальный итоговый образ.
- `compose.yaml`: один сервис `netdoc`, `network_mode: host`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`, `read_only: true`, том для базы, конфиг только на чтение, Docker-сокет только на чтение (в README описан риск доступа к сокету и вариант с `docker-socket-proxy`).
- `compose.lab.yaml`: сеть-мост с фиксированными адресами и контейнеры `snmpd`, `sshd` и `nginx` для проверки ARP, SNMP, SSH и сервисов.
- `goreleaser`: статические бинарники linux/amd64 и linux/arm64.
- CI: `make gate` и `make audit` на каждый pull request, релиз по тегу.
- `SECURITY.md` с порядком сообщения об уязвимостях и `.github/dependabot.yml` для go, npm и docker.
- README по правилам раздела 12: описание, скриншот, установка (бинарник и Docker), создание первого пользователя (`docker compose exec netdoc netdoc user add`), конфиг, TLS и обратный прокси, безопасность (риск Docker-сокета, SNMP v2c открытым текстом), примеры экспорта.

**Критерии приёмки:**

1. `docker compose -f deploy/compose.yaml up --build` из чистого клона поднимает сервис, `GET /healthz` отвечает 200. Остальные маршруты отвечают 503, пока `docker compose exec netdoc netdoc user add` не создаст первого пользователя.
2. `compose.lab.yaml` поднимает лабораторию, скан находит её контейнеры, SNMP-данные от `snmpd`, сервисы от `nginx` и `sshd`.
3. `goreleaser build --snapshot --clean` даёт бинарники linux/amd64 и linux/arm64, `ldd` сообщает, что файл не динамический.
4. CI запускает `make gate` и `make audit` на pull request.
5. README содержит разделы: что это, установка, первый пользователь, конфигурация, безопасность, экспорт. В репозитории есть `SECURITY.md` и `.github/dependabot.yml`.

**Тесты:** интеграционный тест под tag `lab` запускает скан против лаборатории и проверяет число найденных устройств и сервисов. В гейт он не входит.

**Не делать:** Helm и Kubernetes, сборки для Windows и macOS, автообновление, телеметрию.

---

### Слайс 17 - полировка интерфейса

**Файлы:** `web/src/` (тема, состояния загрузки и ошибок), `index.html`, `web/public/theme-init.js`, `web/src/lib/theme.ts`, конфиг eslint (`jsx-a11y`).

**Что делает:** светлая, тёмная и системная тема: класс на `<html>` и значение в `localStorage`, внешний синхронный скрипт `web/public/theme-init.js` в `<head>` ставит класс до отрисовки (inline-скрипты запрещены политикой CSP из раздела 9.4). Состояния загрузки (`skeleton`), ошибки (граница ошибок с кнопкой повтора) и пустоты есть на каждой странице. Клавиша `/` фокусирует поиск на странице инвентаря. Фокус-кольца, `aria`-подписи у узлов и кнопок. Боковая панель сворачивается на узком экране.

**Критерии приёмки:**

1. Выбранная тема применяется без мигания при перезагрузке страницы.
2. Режим «системная» следует настройке ОС и реагирует на её смену.
3. Каждая страница имеет состояния загрузки, пустоты и ошибки.
4. Клавиша `/` на `/inventory` фокусирует поле поиска.
5. `eslint-plugin-jsx-a11y` проходит без ошибок.
6. Интерфейс пригоден для работы при ширине 1280 и 390 пикселей (проверка руками).
7. В `index.html` и в собранных страницах нет inline-скриптов, консоль браузера не показывает нарушений CSP.

**Тесты:** функция определения темы (`theme.ts`) в vitest.

**Не делать:** i18n, брендинг, анимации сверх простых переходов.

---

## 17. Ревью после каждого слайса

Отдельным заходом, после того как слайс готов и гейт зелёный. Задача захода - искать проблемы, а не хвалить написанное.

Чек-лист:

1. SQL только в `internal/store/migrations` и `internal/store/queries`. В других пакетах строк с SQL нет.
2. `collector` не импортирует `store`, `render` не импортирует `collector` и `api`, компоненты не вызывают `fetch`.
3. Имена таблиц, полей, типов и маршрутов совпадают с разделами 4, 5 и 7.
4. Запросов к базе в цикле нет. Список и связанные данные берутся одним запросом или одной транзакцией.
5. Каждая горутина завершается по `ctx`. Нет горутин без выхода, нет записи в закрытый канал.
6. Ошибки возвращаются или логируются, ни одна не игнорируется молча (`_ =` без причины в комментарии).
7. Секреты не попадают в логи, ответы API, экспорт и сообщения об ошибках.
8. Сетевой код уважает границы безопасности из разделов 1 и 9.6: частные диапазоны, режим только чтение, фиксированные команды SSH.
9. Тесты проверяют критерии приёмки, а не повторяют реализацию. На каждый критерий с отказом есть тест на отказ.
10. Использованы компоненты из раздела 6, самописных кнопок и полей в страницах нет.
11. Мёртвого кода нет: неиспользуемые экспорты, SQL-запросы, компоненты, стили.
12. Файлов и абстракций сверх раздела 3 не появилось.
13. Каждый новый маршрут объявлен в таблице маршрутов с правом, прямой регистрации нет, есть тесты на 401 и 403.
14. Данные из сети (hostname, `sysDescr`, имена контейнеров) нормализуются при записи и экранируются при выводе, `dangerouslySetInnerHTML` не используется.
15. Параметры раздела 9 (cookie, заголовки, лимиты, таймауты, сроки сессий и токенов) не ослаблены.

Находки правятся в том же заходе, потом гейт прогоняется заново.

---

## 18. Открытые вопросы (удалить после финализации)

1. **Имя проекта.** Рабочее имя `netdoc`. Нужно финальное имя, оно попадёт в модуль, бинарник, образ и README.
2. **Язык интерфейса.** В черновике интерфейс на английском без i18n. Альтернатива: сразу русский и английский через i18n.
3. **Раскладка топологии.** В черновике одна детерминированная раскладка по слоям на Go, её используют панель и все экспорты. Альтернатива: `elkjs` в интерфейсе для красивой авто-раскладки по кнопке, тогда координаты сохраняются в базе и экспорты берут их оттуда.
4. **PDF.** В черновике через `chromedp` и внешний Chrome. Альтернатива: только стили печати в HTML, без зависимости от браузера. Если Chrome нужен в Docker-образе, образ становится заметно больше, возможен отдельный тег.
5. **Лицензия.** MIT, Apache-2.0 или AGPL. Влияет на репозиторий и README.
6. **SNMP v3.** В черновике только v2c, community идёт открытым текстом (раздел 9.6). SNMP v3 добавляет аутентификацию и шифрование, но усложняет конфиг и хранение секретов. Для чувствительных сетей v3 стоит включить в v1.
7. **Внешняя аутентификация и второй фактор.** В черновике только локальные пользователи с паролем. Варианты: OIDC (единый вход), LDAP, TOTP как второй фактор.
8. **Проверка паролей.** Сейчас только длина и запрет совпадения с именем. Вариант: встроенный список частых паролей для отказа.
9. **Шифрование данных в покое.** База и секреты конфига не шифруются, защита файловыми правами. Варианты: SQLCipher, шифрование секретов мастер-ключом.
10. **Объектные права.** Все пользователи видят всю сеть. Вариант: доступ по сегментам или группам устройств.
11. **Источники обнаружения.** Не вошли: DHCP-аренды (зависят от модели роутера), пассивное прослушивание mDNS, SSDP и NetBIOS, импорт устройств из CSV.
12. **Забывание устройств.** Offline-устройства остаются навсегда. Вариант: `history.forgetAfterDays`.
13. **Графики во времени.** В v1 число устройств по сканам. Трафик интерфейсов (счётчики SNMP) и аптайм вынесены за v1.
14. **PostgreSQL.** В v1 только SQLite. Интерфейсы `Repo` и `AuthRepo` оставляют место для второго бэкенда.
15. **Платформы.** В v1 только Linux.
16. **Подпись релизов.** В v1 файл контрольных сумм. Вариант: подпись `cosign` и SBOM.
