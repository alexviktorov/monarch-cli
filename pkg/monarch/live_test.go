package monarch

// Opt-in live integration tests. They run only when MONARCH_LIVE=1 and use
// the real Keychain session, so they are skipped by default (and in CI).
// They are strictly READ-ONLY — every operation here is a query; no
// mutation is exercised, so nothing in the account can change.
//
//	MONARCH_LIVE=1 go test ./pkg/monarch/ -run TestLive -v
//	# or: make live-test
//
// Purpose: re-verify every read operation's shape against production after
// a Monarch change, in one command, instead of ad-hoc probes.

import (
	"context"
	"os"
	"testing"
	"time"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("MONARCH_LIVE") != "1" {
		t.Skip("live test disabled (set MONARCH_LIVE=1 to run against the real API)")
	}
	store, err := NewKeychainStore()
	if err != nil {
		t.Fatalf("keychain: %v (run `monarch login` first)", err)
	}
	c, err := New(WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	if c.Session() == nil {
		t.Skip("not logged in — run `monarch login` first")
	}
	return c
}

func TestLiveReads(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Session validity first — a dead token makes everything else noise.
	id, err := c.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	t.Logf("session OK: %s", id.Email)

	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Each read is a subtest so one broken shape doesn't hide the others.
	// Counts only — never log balances or transaction detail.
	t.Run("accounts", func(t *testing.T) {
		v, err := c.Accounts.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d accounts", len(v))
	})
	t.Run("transactions", func(t *testing.T) {
		v, err := c.Transactions.Query().Limit(5).Execute(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d of %d transactions", len(v.Transactions), v.TotalCount)
	})
	t.Run("transaction_summary", func(t *testing.T) {
		if _, err := c.Transactions.GetSummary(ctx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("categories", func(t *testing.T) {
		v, err := c.Categories.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d categories", len(v))
	})
	t.Run("tags", func(t *testing.T) {
		v, err := c.Tags.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d tags", len(v))
	})
	t.Run("merchants", func(t *testing.T) {
		v, err := c.Merchants.List(ctx, "", 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d merchants (page)", len(v))
	})
	t.Run("budgets", func(t *testing.T) {
		if _, err := c.Budgets.List(ctx, monthStart, now); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cashflow", func(t *testing.T) {
		if _, err := c.Cashflow.Get(ctx, CashflowParams{StartDate: monthStart, EndDate: now}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cashflow_summary", func(t *testing.T) {
		if _, err := c.Cashflow.GetSummary(ctx, CashflowParams{StartDate: monthStart, EndDate: now}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("recurring", func(t *testing.T) {
		if _, err := c.Recurring.List(ctx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("networth_monthly", func(t *testing.T) {
		if _, err := c.Accounts.GetSnapshots(ctx, SnapshotParams{StartDate: now.AddDate(-1, 0, 0), Timeframe: "month"}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("networth_daily", func(t *testing.T) {
		if _, err := c.Accounts.GetDailySnapshots(ctx, now.AddDate(0, -1, 0), time.Time{}, ""); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("holdings", func(t *testing.T) {
		if _, err := c.Holdings.List(ctx, ""); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("rules", func(t *testing.T) {
		v, err := c.Rules.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d rules", len(v))
	})
	t.Run("goals", func(t *testing.T) {
		v, err := c.Goals.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d goals", len(v))
	})
	t.Run("institutions", func(t *testing.T) {
		if _, err := c.Institutions.List(ctx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("credit_score", func(t *testing.T) {
		if _, err := c.GetCreditScoreHistory(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
