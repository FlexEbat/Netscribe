GO_DIRS := cmd internal web/embed.go
# Explicit packages: ./... would also walk web/node_modules, where npm packages may ship .go files.
GO_PKGS := ./cmd/... ./internal/... ./web
SQLC_QUERIES := $(wildcard internal/store/queries/*.sql)

.PHONY: fmt lint check test gate audit gen dev dev-backend dev-web build

# go:embed needs a file under web/dist before the frontend is built.
web/dist:
	mkdir -p web/dist
	touch web/dist/.gitkeep

web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci
	touch web/node_modules

fmt: web/node_modules
	gofmt -w $(GO_DIRS)
	cd web && npm run format

lint: web/dist web/node_modules
	@out="$$(gofmt -l $(GO_DIRS))"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet $(GO_PKGS)
	golangci-lint run $(GO_PKGS)
ifneq ($(SQLC_QUERIES),)
	sqlc diff
endif
	cd web && npm run lint && npm run format:check

check: web/node_modules
	cd web && npm run typecheck

test: web/dist web/node_modules
	go test -race $(GO_PKGS)
	cd web && npm test

gate: lint check test

# Needs network access, runs in CI, not part of gate.
audit:
	govulncheck $(GO_PKGS)
	cd web && npm audit --omit=dev --audit-level=high

gen:
ifneq ($(SQLC_QUERIES),)
	sqlc generate
else
	@echo "no queries yet, nothing to generate"
endif

dev: web/node_modules
	@test -f netscribe.yaml || { cp netscribe.example.yaml netscribe.yaml && chmod 600 netscribe.yaml; }
	@$(MAKE) -j2 dev-backend dev-web

dev-backend: web/dist
	air -c .air.toml

dev-web:
	cd web && npm run dev

build: web/node_modules
	cd web && npm run build
	go build -o netscribe ./cmd/netscribe
