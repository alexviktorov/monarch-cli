# monarch — CLI + MCP server for Monarch Money

A personal Go CLI and MCP server for Monarch Money's (unofficial) GraphQL
API — the same API the web app uses — so you can pull accounts, transactions,
budgets, cashflow, recurring bills, and net worth from the terminal, scripts,
or an LLM connected over MCP.

The API client is written from scratch in `pkg/monarch` with **zero
third-party dependencies** (stdlib + `golang.org/x/term` for prompts): no
telemetry, no community client libraries, the only host contacted is
`api.monarch.com`. See `.supply-chain-risk-auditor/results.md` for the audit
that motivated this and UPSTREAM.md for how upstream fixes are tracked
without importing code.

> ⚠️ This is an **unofficial** API. Monarch can change or break it at any
> time, and heavy automated use is at your own risk.

## Install

Build from source:

```sh
go build -o monarch .
```

## Login

```sh
monarch login
# Email: you@example.com
# Password: ********          (never echoed; requires a TTY)
# Two-factor code: 123456     (if MFA is enabled)
```

On macOS the session token is stored in your **login Keychain** (service
`monarch-cli`), encrypted at rest; an existing `~/.config/monarch/session.json`
from older versions is migrated in automatically. Elsewhere (or with
`MONARCH_SESSION_FILE=/path`) it's a 0600 JSON file. Honest caveat: because
the item is written via Apple's `security` tool, any process running *as you*
can read it back the same way — Keychain protects the token at rest and while
the keychain is locked, not against same-user malware.

Alternatives:

- `monarch login --totp` — generates the 2FA code from your TOTP secret,
  read from `MONARCH_TOTP_SECRET` or a hidden prompt (never a CLI argument —
  argv is visible to other processes).
- `monarch login --password-stdin` — read the password from stdin for
  scripted use.
- `MONARCH_TOKEN=<token>` — use a bearer token you extracted yourself (web
  app devtools: any GraphQL request → `Authorization: Token <token>` header).

`monarch whoami` shows the current session; `monarch logout` deletes it.

## Commands

Every data command accepts `--json` for raw output (pipe into `jq`).

```sh
monarch accounts                  # balances by account + net worth line
monarch transactions --start 2026-07-01 --end 2026-07-31 --limit 100
monarch transactions --search "whole foods" --min 20
monarch transactions --category <id> --account <id>   # IDs from `categories`/`accounts`
monarch summary                   # lifetime totals
monarch budget --month 2026-07    # budget vs. actual per category
monarch cashflow --start 2026-01-01 --end 2026-06-30 --top 15
monarch categories                # category IDs for filtering
monarch recurring                 # subscriptions/bills + est. monthly total
monarch networth --start 2025-08-01 --timeframe month
```

Sign convention follows Monarch: expenses are negative amounts, income
positive. `--min`/`--max` bound the *absolute* amount.

## MCP server

`monarch mcp` serves an MCP server on stdio with nine read tools
(`get_accounts`, `get_transactions`, `get_transaction_summary`, `get_budget`,
`get_cashflow`, `get_categories`, `get_tags`, `get_recurring`,
`get_networth_history`).

Register with Claude Code (read-only — recommended default):

```sh
go build -o ~/bin/monarch .
claude mcp add monarch --scope user -- ~/bin/monarch mcp
```

Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "monarch": { "command": "/Users/you/bin/monarch", "args": ["mcp"] }
  }
}
```

### Writes (off by default)

One write tool exists — `update_transaction` (category, notes, tags). It is
**not even registered** unless the server is started with *both* the
`--allow-writes` flag and `MONARCH_MCP_ALLOW_WRITES=1`; a mismatch refuses to
start. Register it as a separate server entry and only while you're actively
doing a cleanup session:

```sh
claude mcp add monarch-rw --scope user --env MONARCH_MCP_ALLOW_WRITES=1 -- \
  ~/bin/monarch mcp --allow-writes
```

Every tool call is rate-limited (30/min), capped at 3 concurrent API calls,
and timed out at 30s; logs go to stderr and never contain the token.

## Development

```sh
make check     # gofmt + vet + build + tests + govulncheck — required when deps change
go test . -update   # regenerate golden files after intentional output changes
```

Architecture, the Monarch API contract (headers, MFA protocol, GraphQL
quirks), and security invariants are documented in CLAUDE.md. Upstream
drift-monitoring policy and watchlist: UPSTREAM.md.
