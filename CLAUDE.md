# monarch-cli

Personal Go library + CLI + MCP server for Monarch Money's unofficial GraphQL
API (the same API the web app uses). Built as a fallback for when the official
Monarch MCP is down, with a supply-chain posture suited to live financial data.

## Commands

```sh
go build -o monarch .     # build
make check                # gofmt + vet + build + test + govulncheck (MANDATORY when go.mod/go.sum change)
./monarch help            # usage
./monarch mcp             # stdio MCP server (read-only by default)
```

## Decision record (2026-08): clean-room client, no third-party Monarch deps

This repo previously depended on `github.com/eshaffer321/monarch-go/v2`. A
supply-chain audit (2026-08) found no malicious code but an unacceptable
posture for financial data: 3 stars, zero known
importers, bus factor 1, no security contact, a vulnerable transitive pin, and
a Sentry global-hub capture path (full GraphQL query + variables on every
error, no opt-out) that any `sentry.Init` in the process would silently arm.
Decision: **the API layer is written from scratch in `pkg/monarch` (pure
stdlib) and stays that way.** We monitor upstream libraries for API-breakage
fixes and re-implement from observed behavior — see UPSTREAM.md. Never add a
Monarch client dependency; never copy upstream code.

Allowed dependencies: stdlib, `golang.org/x/term` (prompts), and
`github.com/modelcontextprotocol/go-sdk` ≥ v1.7.0 (MCP protocol only; stdio
transport only — its 2026 HIGH advisories were all HTTP-transport bugs).

## Architecture

- `pkg/monarch/` — importable, pure-stdlib client library.
  - `transport.go` — `doGraphQL` core; retry (4 attempts, exp backoff ±25%
    jitter, honors Retry-After) for reads; `doGraphQLNoRetry` for writes.
  - `auth.go`, `session.go`, `keychain_darwin.go` — login/MFA, SessionStore
    (Keychain on macOS via `security -i` with the payload on **stdin**;
    atomic symlink-refusing 0600 file store elsewhere).
  - one file per domain (`accounts.go`, `transactions.go`, …) — each holds its
    own minimal GraphQL documents as consts; request only fields we consume.
  - `writes.go` — ALL mutations live here, gated by `WithWritesEnabled`,
    no-retry, payload-error checked.
- Root `package main` — CLI: `main.go` dispatch, `client.go` store resolution,
  `login.go`, `commands.go` (thin wrappers), `render.go` (pure renderers,
  golden-tested), `output.go`.
- `mcp.go`, `mcp_tools.go`, `redact.go` — MCP server: 9 read tools;
  `update_transaction` registered only when `--allow-writes` AND
  `MONARCH_MCP_ALLOW_WRITES=1` (mismatch = startup error). Middleware: rate
  limit 30/min burst 10, 3-way concurrency cap, 30s timeout, redaction.

## API contract (verified 2026-08; re-verify via devtools when things break)

- Base `https://api.monarch.com` (NOT the old api.monarchmoney.com);
  GraphQL `POST /graphql`; login `POST /auth/login/` (trailing slash matters).
- Headers on every request: `Client-Platform: web`, `Origin`/`Referer`
  app.monarch.com, `device-uuid` — login 404s without them
  (hammem/monarchmoney#156). GraphQL auth: `Authorization: Token <t>` (not
  Bearer).
- Login body: `{username, password, trusted_device: true, supports_mfa: true,
  supports_email_otp: true, supports_recaptcha: true}`. MFA is signaled by
  `error_code` in the response body (`MFA_REQUIRED` / `EMAIL_OTP_REQUIRED` /
  `INVALID_CREDENTIALS`), checked BEFORE HTTP status — NOT by 403. Retry the
  same endpoint with `totp` (or `email_otp`) added, same `device-uuid` as the
  first attempt. Logins and mutations are never auto-retried.
- The server never reports a token lifetime; we store no expiry and treat a
  real 401 as the truth (upstream libs fabricate login+24h — don't).
- Quirks pinned by tests: `aggregates` is an ARRAY (`GetTransactionsPage`);
  the transactions `filters` variable must be sent even when empty; budget
  `Spent = -actualAmount` — only meaningful for expense rows, so income-group
  rows (BudgetRow.GroupType == "income") are excluded from spend totals;
  cashflow `Expense = -sumExpense` and its filters MUST include empty
  `search`/`categories`/`accounts`/`tags` keys; recurring items flatten
  `stream{frequency merchant}`; transaction min/max amount bounds are
  client-side on absolute value (post-pagination — TransactionList.Fetched
  records the pre-filter page size); mutation payload `errors` can be an
  object OR an array (payloadErrors handles both).
- Auth robustness (Tier-1 patch, 2026-08-09): token-only sessions derive a
  STABLE device-uuid from the token (never random per run — Monarch ties
  tokens to device identity); `error_code == "CAPTCHA_REQUIRED"` (and prose
  `detail` fallbacks) map to ErrCaptchaRequired, never "invalid password";
  every request sends `monarch-client`/`monarch-client-version` (recapture
  procedure in UPSTREAM.md); JWT-shaped login tokens are rejected (that's
  the 1-hour features token); `Client.Ping` (GetIdentity) backs the live
  `whoami` check and the `check_session` MCP tool; redirects are refused.
- Operation catalog beyond the original 8: `GetTransactionDetails` (drawer
  incl. splits), `Web_GetHoldings`, `GetAggregateSnapshots` (daily),
  `Web_GetCashFlowSummary`, `GetTransactionRules` (criteria kept as raw
  JSON — shape unpinned), `Common_SavingsGoals`, `GetInstitutions`,
  `GetIdentity`; mutations `Web_TransactionDrawerUpdateTransaction`
  (extended field set), `Web_SetTransactionTags`,
  `Common_CreateTransactionTag`, `Common_SplitTransactionMutation`,
  `Common_CreateTransactionMutation`, `Common_UpdateBudgetItem`,
  `Common_UpdateMerchant`, `Web_CreateCategory`/`Web_UpdateCategory`/
  `Web_DeleteCategory`, `Common_Create/UpdateTransactionRuleMutationV2`,
  `Common_DeleteTransactionRule`. The `needsReview` filter key is
  live-verified (2026-08-09).
- Mutation quirks pinned by tests: budget-set key is `applyToFuture` (never
  applyToFutureMonths) and its payload has NO errors field (missing
  budgetItem = failure); category create's group key is `group`; category/
  rule deletes use BARE variables not input objects; rule actions are bare
  values on the wire (category ID string, merchant NAME string, tag ID
  list) while rule READS return objects — never round-trip; rule create/
  update return errors only (no id — the MCP tool diffs get_rules around
  the create); rule delete's `deleted` can be null ON SUCCESS (only
  explicit false or an errors payload is failure); splits are full-replace
  with server-enforced sum==parent (MCP pre-validates); create-transaction
  requires categoryId and rounds amount to 2dp.
- Sign convention: expenses negative, income positive. `monthlyEstimate` and
  the cashflow/recurring renderers rely on it.

## Security invariants (do not weaken)

- Token never in argv (Keychain writes go through `security -i` stdin), never
  logged, never in error strings; `APIError.Error()` is status+operation only;
  MCP has a redaction backstop and logs to stderr exclusively.
- TOTP seed comes from `MONARCH_TOTP_SECRET` or a no-echo prompt — never a
  flag.
- Secret prompts refuse non-TTY stdin unless `--password-stdin`.
- Session file: 0600, atomic rename, symlink-refusing, group/world-writable
  parents refused.
- Mutations: only in `writes.go`/`update_transaction`, only behind the
  dual-key gate, never retried. Adding a new write requires the owner
  explicitly asking.
- Zero telemetry. `go mod graph | grep -Ei 'sentry|analytics'` must stay
  empty. Run `make check` whenever dependencies change.

## Testing

- `pkg/monarch`: httptest fixtures per operation (sign conventions pinned),
  RFC 6238 vectors, login/MFA protocol, keychain via injected `run` fake
  (asserts token absent from argv), session-store permission tests.
- Root: golden-file render tests (`go test . -update` to regenerate),
  MCP in-memory transport tests (inventory gating, redaction, rate limit).
- Live smoke (needs a real session): all 8 read commands ± `--json`, login
  via TOTP-env and interactive MFA, `security find-generic-password -s
  monarch-cli` shows the item after login, logout removes it; MCP read
  sequence + gated write test (update one transaction's notes, revert).

## Backlog (ranked remainder of the 2026-08 upstream comparison; pick up on request)

Read-only:
- Name→ID resolution for --account/--category (read-side only; the official
  MCP deliberately refuses name matching on writes)
- MCP elicitation-based login (Go SDK elicitation; removes the
  "run `monarch login` in a terminal" dead end for GUI hosts)
- wide-search fallback (opt-in bounded local scan when server `search`
  misses notes/original-statement; robcerda tools/transactions.py)
- Month-over-month diff / burn-rate analysis (local computation only)
- Category groups (`ManageGetCategoryGroups`), subscription details
  (`GetSubscriptionDetails`), account detail (`AccountDetails_getAccount`),
  per-account balance history (`GetAccountHistory`), recent balances
  (`GetAccountRecentBalances`), credit score (`GetCreditScoreSnapshots`)
- Scheduled routine wrapping scripts/upstream-check.sh (summarize-only)

Writes (each needs explicit owner approval; dry-run/confirm pattern first):
- Delete transaction: `Common_DeleteTransactionMutation` (input
  `{transactionId}`; deleted+errors response)
- Account refresh: `Common_ForceRefreshAccountsMutation` (input
  `{accountIds}`) + poll `ForceRefreshAccountsQuery` (hasSyncInProgress) —
  hits third-party institutions, deserves its own gate; watch for
  credential.updateRequired to avoid polling forever
- Manual accounts CRUD: `Web_CreateManualAccount` (type/subtype/
  includeInNetWorth/name/displayBalance), `Common_UpdateAccount` (⚠️
  hideFromList vs hideFromSummaryList input key unresolved — probe live),
  `Common_DeleteAccount($id: UUID!)`
- Holdings CRUD: `Common_CreateManualHolding`/`Common_UpdateHoldingMutation`
  /`Common_DeleteHolding` + `SecuritySearch` two-step with exact-ticker
  match guard
- Balance-history CSV upload: REST multipart, hammem vs monarch-go form
  fields disagree — devtools verification required (see UPSTREAM.md)
