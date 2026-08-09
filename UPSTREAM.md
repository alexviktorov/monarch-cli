# Upstream drift monitoring

**Policy: we LEARN from upstream, we never import it.** When an upstream
project fixes Monarch API breakage: read the diff or issue to understand what
Monarch changed, verify against the live API yourself (app.monarch.com →
devtools → Network tab), then re-implement in our own code from the observed
behavior. Never vendor, copy, or transcribe upstream code — for licensing
provenance, and because understanding the fix is the point.

This repo deliberately has no third-party Monarch dependency (see the
2026-08 decision record in CLAUDE.md). The projects below are eyes, not
dependencies.

## Watchlist

| Repo | Why watch | What to watch |
|---|---|---|
| `hammem/monarchmoney` (Python) | Largest user base (~520★) — Monarch breakage lands in its **issue tracker** first (issue #156 was the api.monarch.com + `device-uuid` change). The owner is stalled, so watch issues/PRs, **not** commits; fixes appear in PR diffs and comments long before merge | issues/PRs: 403, 404, login, headers |
| `eshaffer321/monarch-go` (Go) | The dependency this repo removed (2026-08). Closest code shapes to ours; its `internal/graphql/queries/` remains a useful operation catalog to *read* | commits touching auth/transport/queries |
| `robcerda/monarch-mcp-server` (Python) | Highest-star Monarch MCP server — tool-design prior art and a second early-warning channel | issues |
| `thedavidweng/monarchmoney-cli` (Go) | The other active Go Monarch CLI. If Monarch changes something, a second Go implementation's fix shows exactly which behavior moved; divergence between us and them is a signal one of us is wrong | commits |
| `wesm/moneyflow` (Python) | Multi-backend personal-finance tool with a Monarch adapter; multi-backend projects notice and document provider changes quickly | adapter commits |
| `modelcontextprotocol/go-sdk` (Go) | Our MCP protocol dependency. **Subscribe to its GitHub security advisories** — it shipped four HIGH advisories in 2026 (all ≤1.4.x, all patched; three were HTTP-transport-only, which our stdio-only server sidesteps). Re-review at each minor bump | releases + security advisories |

## Dependency-scrutiny note

The MCP SDK pulls `github.com/segmentio/encoding` (a faster JSON codec used
to parse protocol frames + tool-call arguments), which in turn pins
`github.com/segmentio/asm` v1.1.3 — a hand-written SIMD assembly library,
dormant since late 2023, sitting in the JSON-decode hot path for
model/attacker-influenced input. No CVE/advisory and it's a legitimate
Twilio-Segment library, but it's the one higher-scrutiny transitive dep;
it can't be bumped without a `replace` until segmentio/encoding updates its
pin. Watch for a segmentio/encoding release that moves to asm v1.2.1+.
Re-run `make check` (govulncheck) on every go-sdk bump.

## Ecosystem status notes (2026-08-09)

- **hammem `main` is pinned to the dead `api.monarchmoney.com` host** — its
  code is stale; its issue tracker remains the best breakage signal. For
  *live auth behavior*, **robcerda/monarch-mcp-server's `monarch_auth.py` is
  currently the ecosystem's best-maintained reference** (CAPTCHA handling,
  token-shape validation, client-version headers, device-uuid persistence).
- Balance-history CSV upload (`POST /account-balance-history/upload/`) is
  NOT implemented here; note for whenever it is: hammem and monarch-go
  disagree on the multipart form fields (`files`+`account_files_mapping`
  vs `account_id`+`file`), and hammem's version has a request bug — verify
  against devtools before trusting either.

## Recapturing monarch-client-version

Monarch validates `monarch-client` / `monarch-client-version` against a
server-side minimum; a stale version can start returning 403s. The default
lives in `pkg/monarch/transport.go` (`defaultClientVersion`). To recapture:
open app.monarch.com → devtools → Network → any `graphql` request → copy the
`monarch-client-version` request header value, bump the constant (or set
`MONARCH_CLIENT_VERSION` as a stopgap), and log the change below.

## Signals that matter

- Commits/issues touching: login, auth, headers, `device-uuid`,
  `Client-Platform`, base URL (`api.monarch.com`), GraphQL operation names,
  Cloudflare, MFA/OTP.
- New required headers or renamed/removed GraphQL operations.

## Cadence

- **Monthly**: run `scripts/upstream-check.sh` (needs `gh`). Non-zero exit
  means an auth-flagged commit was found.
- **Immediately** on any unexplained 4xx from our own client — someone
  upstream has usually already diagnosed it. Check the watchlist before
  debugging blind.
- Optional automation: a scheduled Claude Code routine can run the script
  monthly and summarize *what Monarch changed* (API behavior only — it must
  propose no code and import nothing; re-implementation is always a
  deliberate manual step).

## Change log

Record every drift fix here: date — upstream ref (link) — what Monarch
changed — our commit re-implementing it.

| Date | Upstream ref | What Monarch changed | Our commit |
|---|---|---|---|
| 2026-08-09 | (observed directly in live smoke; server 400s with query locations) | `snapshotsByAccountType` field `sum` renamed to `balance`; `aggregates` no longer accepts a `limit` argument; `CreateTransactionTagInput.color` is REQUIRED (every upstream catalog still shows the old shapes) | fix in pkg/monarch accounts.go/cashflow.go/writes.go |
| 2026-08-09 | (observed directly; worse than robcerda's documented null-on-success quirk) | `deleteTransactionRule` returned `deleted: false` for a deletion that SUCCEEDED — the flag is unreliable in both directions; only the errors payload signals failure | Rules.Delete ignores `deleted` entirely |
| 2026-08-09 | (extracted from the live app.monarch.com bundle + probes; every OSS catalog had these wrong or absent) | tag delete is `Common_DeleteHouseholdTransactionTag($tagId: ID!)` (arg `tagId`, errors-only — no `deleted`); tag update is `Common_UpdateTransactionTag($input: UpdateTransactionTagInput!{id,name,color})`; **merchant merge = `deleteMerchant(id, moveRelationsToMerchantId){success}`** — there is NO `mergeMerchants` mutation; merchants list is `merchants(search,limit,offset)` | added to writes.go/merchants.go |
| 2026-08-09 | (bundle + fake-id forensic probe) | goal moves are `Common_ContributeToSavingsGoal`/`Common_WithdrawFromSavingsGoal($input{id,accountId,amount,date?})`; they are LENIENT — an unknown goal/account id returns HTTP 200 with `goalEvent: null` and moves nothing, so a null event MUST be treated as failure | Goals.Contribute/Withdraw guard on null goalEvent |
