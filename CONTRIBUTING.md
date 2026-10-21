# Contributing to Netscribe

Thank you for helping. This file explains how to set up, what a good change looks like and
how a pull request gets merged. Coding agents follow [AGENTS.md](AGENTS.md) in addition.

## Report a problem or an idea

Open an issue. For a bug, give the version or commit, your distribution, what you did, what
happened and what you expected, and paste the log lines around the failure. The log masks
secrets, but read it before you post it: it can hold host names and addresses of your network.

Report a security problem privately, as described in [SECURITY.md](SECURITY.md). Do not open
a public issue for it.

## Set up

You need Go (the version in `go.mod`), Node.js 22, `git` and `make`. On a machine that has
none of them:

```sh
sh scripts/install-build-deps.sh
```

The script works on Debian, Ubuntu, Fedora, RHEL and its clones, openSUSE, Arch, Alpine and
Void. It installs `git` and `make` with the package manager and takes Go and Node.js from
their official archives after it checks the checksums. For lint, codegen and the vulnerability
scan install three more tools with Go:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go install golang.org/x/vuln/cmd/govulncheck@latest
```

Then:

```sh
git clone https://github.com/FlexEbat/Netscribe && cd Netscribe
make build        # frontend, then the binary ./netscribe
make dev          # backend with reload and the Vite dev server
```

`make dev` also needs [air](https://github.com/air-verse/air) for the reload:
`go install github.com/air-verse/air@latest`.

`go run ./cmd/seed --dev-user` fills a database with a demo network and creates the account
`dev` with the role `admin`. The password comes from the variable `NETSCRIBE_DEV_PASSWORD`.
There is no default password.

## Checks

```sh
make gate         # what CI runs on a pull request: lint, typecheck, tests
make audit        # govulncheck and npm audit, needs the network
```

To test a scan against known hosts, start the lab and run the lab tests:

```sh
docker compose -f deploy/compose.lab.yaml up --build -d
go test -tags lab ./cmd/netscribe -run Lab
```

Run scans only against networks you own or against the lab.

## Make a change

1. Read the part of [tech.md](tech.md) that your change touches. Names, routes, the config
   format and the security parameters of section 9 are fixed there. If your change needs a
   new one, say so in the issue first.
2. Branch from `main`. Keep the change small and about one thing.
3. Write the test from the behavior you want, and watch it fail before the fix. Do not wait
   for events with a fixed sleep in a test: wait on a channel or a poll with a deadline.
4. Run `make gate`. If you changed `internal/store/queries`, run `make gen` and commit the
   generated code.
5. Commit with the convention `type(scope): summary` that [AGENTS.md](AGENTS.md) lists.
6. Open a pull request. Say what changed and why, and how you checked it. CI must be green
   before a review starts.

## What a review looks for

The checklist of tech.md section 17: SQL only where it belongs, the import rules, no query
in a loop, every goroutine ends with its context, no error dropped without a reason, no
secret in logs or exports, network code inside the allowed ranges, tests for every refusal,
no dead code, a permission and tests for 401 and 403 on every route, and untrusted text that
is escaped on output.

## Documentation

Change the documentation in the same pull request as the code. User-facing text lives in
the README and in `docs/`. Write short sentences in the active voice and test every command
you put in them.

## License

By contributing you agree that your work is licensed under the MIT license of the project.
