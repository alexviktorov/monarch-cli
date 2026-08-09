package main

import (
	"fmt"
	"os"
)

const usage = `monarch — CLI for your Monarch Money data (unofficial API)

USAGE
  monarch <command> [flags]

AUTH
  login          Log in (email/password + MFA) and save a session
  logout         Delete the saved session
  whoami         Show the current session

DATA (all support --json)
  accounts       List accounts with balances        [--all]
  transactions   List transactions                  [--start --end --limit --offset
                                                     --search --account --category
                                                     --min --max]
  summary        Lifetime transaction summary
  budget         Budget vs. actual for a month      [--month YYYY-MM]
  cashflow       Income/expense + top categories    [--start --end --top N]
  categories     List category IDs (for filtering)
  recurring      Recurring subscriptions & bills
  networth       Net worth history                  [--start --timeframe month|year]

MCP
  mcp            Serve an MCP server on stdio       [--allow-writes]
                 (writes also need MONARCH_MCP_ALLOW_WRITES=1)

ENVIRONMENT
  MONARCH_TOKEN         Use a bearer token directly (skips saved session)
  MONARCH_SESSION_FILE  Override session file path
                        (default: ~/.config/monarch/session.json)

EXAMPLES
  monarch login
  monarch transactions --start 2026-07-01 --end 2026-07-31 --limit 100
  monarch cashflow --start 2026-01-01 --end 2026-06-30 --top 15
  monarch budget --month 2026-07
  monarch accounts --json | jq '.[].displayName'
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "login":
		cmdLogin(args)
	case "logout":
		cmdLogout(args)
	case "whoami":
		cmdWhoami(args)
	case "accounts":
		cmdAccounts(args)
	case "transactions", "tx":
		cmdTransactions(args)
	case "summary":
		cmdSummary(args)
	case "budget":
		cmdBudget(args)
	case "cashflow":
		cmdCashflow(args)
	case "categories":
		cmdCategories(args)
	case "recurring":
		cmdRecurring(args)
	case "networth":
		cmdNetworth(args)
	case "mcp":
		cmdMCP(args)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}
