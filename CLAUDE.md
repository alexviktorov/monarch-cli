# monarch-cli

Read-only Go CLI for Monarch Money's unofficial GraphQL API (the same API the
web app uses). Built as a fallback for when the Monarch MCP server is down.

## Commands

```sh
go build -o monarch .     # build
go vet ./...              # lint
go test ./...             # tests (money formatter has coverage; add more)
./monarch help            # usage
```

## Architecture (intentionally flat, package main)

- `main.go` — command dispatch + usage text
- `client.go` — client construction; auth resolution: MONARCH_TOKEN env → saved session file (`~/.config/monarch/session.json`, override with MONARCH_SESSION_FILE)
- `login.go` — login/logout/whoami; handles TOTP secret, interactive MFA code, email OTP
- `commands.go` — all data commands (accounts, transactions, summary, budget, cashflow, categories, recurring, networth); every command takes `--json`
- `output.go` — tabwriter tables, `money()` formatting, printJSON, fatal

## Key dependency — do not hand-roll the API layer

All Monarch API calls go through `github.com/eshaffer321/monarch-go/v2`
(MIT, actively maintained). It encodes the fragile parts of this unofficial API:

- Base URL is `api.monarch.com` (NOT the old `api.monarchmoney.com`)
- Login: `POST /auth/login/` needs `Client-Platform: web` AND a `device-uuid`
  header — without it Monarch 404s (hammem/monarchmoney#156)
- MFA: 403/"MFA required" → retry with `totp` field; email OTP variant exists
- GraphQL documents live in the lib under `internal/graphql/queries/` — use as
  the catalog when adding commands

When Monarch breaks something, prefer `go get -u github.com/eshaffer321/monarch-go/v2`
over local patches. Deliberate decision (2026-08): depend on the maintained lib
rather than rewrite it; revisit only if it goes stale.

## Gotchas

- go.mod contains `replace golang.org/x/* => github.com/golang/*` lines. These
  were needed to build in a network-sandboxed environment and are safe but
  unnecessary on a normal network. Suggested first chore: delete the replace
  block and `go mod tidy`.
- Sign convention is Monarch's: expenses negative, income positive. The
  cashflow/recurring commands rely on this when filtering/aggregating.
- `networth` classifies types {credit, loan, other_liability} as liabilities
  and uses abs() so it's correct under either sign convention Monarch returns.
- Keep this CLI **read-only**. It's an unofficial API against live financial
  data; no mutation commands without the owner explicitly asking.
- Never log or print the token; session file is chmod 0600.

## Backlog ideas (owner-approved directions, pick up on request)

- `tags` command; `holdings <accountID>`; CSV export flag
- Accept account/category *names* (resolve to IDs via list calls)
- Self-contained zero-dependency client (~10 GraphQL calls inlined, MIT
  attribution) if the sentry-go transitive dep or lib staleness becomes a concern
- Month-over-month spending diff command for budget reviews
