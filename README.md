# monarch — CLI for Monarch Money

A small Go CLI that talks to Monarch Money's (unofficial) GraphQL API — the same
API the web app uses — so you can pull accounts, transactions, budgets, cashflow,
recurring bills, and net worth from the terminal or scripts, without the MCP server.

It builds on [eshaffer321/monarchmoney-go](https://github.com/eshaffer321/monarchmoney-go),
an actively maintained Go client that handles the current auth quirks
(`api.monarch.com` base URL, `device-uuid` header, MFA / email-OTP flows).

> ⚠️ This is an **unofficial** API. Monarch can change or break it at any time,
> and heavy automated use is at your own risk. Read-only commands only.

## Install

Prebuilt binaries are included (`monarch-darwin-arm64` for Apple Silicon Macs).
Or build from source (Go 1.21+):

```sh
go build -o monarch .
```

Note: `go.mod` contains `replace` directives pointing `golang.org/x/*` at their
GitHub mirrors (needed to build in a sandboxed environment). On a normal network
you can delete those lines and run `go mod tidy` — the code is identical.

On macOS, the first run of an unsigned downloaded binary may be blocked by
Gatekeeper; clear it with `xattr -d com.apple.quarantine ./monarch-darwin-arm64`.

## Login

```sh
monarch login
# Email: you@example.com
# Password: ********
# Two-factor code: 123456        (if you have MFA enabled)
```

The session token is saved to `~/.config/monarch/session.json` (mode 0600) and
reused by every other command. Alternatives:

- `monarch login --totp-secret ABC123...` — generates the 2FA code from your TOTP secret
- `MONARCH_TOKEN=<token>` — use a bearer token you extracted yourself (from the web
  app's devtools: any GraphQL request → `Authorization: Token <token>` header);
  the CLI never sees your password this way
- `MONARCH_SESSION_FILE=/path` — override the session file location

`monarch whoami` shows the current session; `monarch logout` deletes it.

## Commands

Every data command accepts `--json` for raw output (pipe into `jq`).

```sh
monarch accounts                  # balances by account + net worth line
monarch transactions --start 2026-07-01 --end 2026-07-31 --limit 100
monarch transactions --search "whole foods" --min -500
monarch transactions --category <id> --account <id>   # IDs from `categories`/`accounts`
monarch summary                   # lifetime totals
monarch budget --month 2026-07    # budget vs. actual per category
monarch cashflow --start 2026-01-01 --end 2026-06-30 --top 15
monarch categories                # category IDs for filtering
monarch recurring                 # subscriptions/bills + est. monthly total
monarch networth --start 2025-08-01 --timeframe month
```

Sign convention follows Monarch: expenses are negative amounts, income positive.

## How the API works (if you want to extend it)

- **Auth**: `POST https://api.monarch.com/auth/login/` with JSON
  `{username, password, supports_mfa: true, trusted_device: false}` plus headers
  `Client-Platform: web` and a random `device-uuid`. MFA retries the same call
  with `totp` (or `email_otp`) added. Response contains `{token}`.
- **Everything else**: `POST https://api.monarch.com/graphql` with
  `Authorization: Token <token>`. The GraphQL documents used here live in the
  library under `internal/graphql/queries/` — useful as a catalog of operations
  (`GetAccounts`, `GetTransactionsList`, `GetJointBudgets`, `snapshotsByAccountType`, …).

## Files

| file          | purpose                                    |
|---------------|--------------------------------------------|
| `main.go`     | command dispatch + usage                   |
| `client.go`   | client construction, session/token resolution |
| `login.go`    | login / logout / whoami (MFA + email OTP)  |
| `commands.go` | all read commands                          |
| `output.go`   | tables, `$`-formatting, JSON output        |
