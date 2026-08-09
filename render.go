package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"monarch-cli/pkg/monarch"
)

// Pure rendering: every function writes a finished table/report to w and
// touches nothing else. commands.go supplies the data; golden tests in
// render_test.go pin the output.

func renderAccounts(w io.Writer, accounts []*monarch.Account, all bool) {
	t := newTable(w, "ID", "NAME", "TYPE", "INSTITUTION", "BALANCE", "UPDATED")
	var total float64
	for _, a := range accounts {
		if a.IsHidden && !all {
			continue
		}
		typ, inst := "", ""
		if a.Type != nil {
			typ = a.Type.Display
		}
		if a.Institution != nil {
			inst = a.Institution.Name
		}
		if a.IncludeInNetWorth {
			total += a.DisplayBalance
		}
		t.row(a.ID, truncate(a.DisplayName, 32), typ, truncate(inst, 24),
			money(a.DisplayBalance), a.DisplayLastUpdatedAt.Format("2006-01-02"))
	}
	t.flush()
	fmt.Fprintf(w, "\nNet worth (included accounts): %s\n", money(total))
}

func renderTransactions(w io.Writer, list *monarch.TransactionList) {
	t := newTable(w, "DATE", "MERCHANT", "CATEGORY", "ACCOUNT", "AMOUNT")
	for _, tx := range list.Transactions {
		merchant := tx.PlaidName
		if tx.Merchant != nil && tx.Merchant.Name != "" {
			merchant = tx.Merchant.Name
		}
		cat := ""
		if tx.Category != nil {
			cat = tx.Category.Name
		}
		acct := ""
		if tx.Account != nil {
			acct = tx.Account.DisplayName
		}
		t.row(tx.Date.Format("2006-01-02"), truncate(merchant, 30),
			truncate(cat, 22), truncate(acct, 22), money(tx.Amount))
	}
	t.flush()
	fmt.Fprintf(w, "\nShowing %d of %d transactions", len(list.Transactions), list.TotalCount)
	if list.HasMore {
		fmt.Fprintf(w, " (next: --offset %d)", list.NextOffset)
	}
	fmt.Fprintln(w)
}

func renderSummary(w io.Writer, s *monarch.TransactionSummary) {
	fmt.Fprintf(w, "Transactions: %d (%s to %s)\n", s.Count, s.First, s.Last)
	fmt.Fprintf(w, "Total income:  %s\n", money(s.SumIncome))
	fmt.Fprintf(w, "Total expense: %s\n", money(s.SumExpense))
	fmt.Fprintf(w, "Average:       %s   Largest expense: %s\n", money(s.Avg), money(s.MaxExpense))
}

func renderBudget(w io.Writer, budgets []*monarch.BudgetRow, monthLabel string) {
	t := newTable(w, "CATEGORY", "BUDGET", "SPENT", "REMAINING")
	var totBudget, totSpent float64
	for _, b := range budgets {
		name := b.CategoryID
		if b.Category != nil {
			name = b.Category.Name
		}
		totBudget += b.Amount
		totSpent += b.Spent
		t.row(truncate(name, 28), money(b.Amount), money(b.Spent), money(b.Remaining))
	}
	t.row("TOTAL", money(totBudget), money(totSpent), money(totBudget-totSpent))
	t.flush()
	fmt.Fprintf(w, "\nBudget for %s\n", monthLabel)
}

func renderCashflow(w io.Writer, cf *monarch.Cashflow, start, end time.Time, top int) {
	fmt.Fprintf(w, "Cashflow %s → %s\n\n", start.Format("2006-01-02"), end.Format("2006-01-02"))
	if cf.Summary != nil {
		fmt.Fprintf(w, "Income:       %s\n", money(cf.Summary.Income))
		fmt.Fprintf(w, "Expenses:     %s\n", money(cf.Summary.Expense))
		fmt.Fprintf(w, "Savings:      %s (%.1f%%)\n\n", money(cf.Summary.Savings), cf.Summary.SavingsRate*100)
	}

	if len(cf.ByCategory) > 0 {
		cats := make([]*monarch.CashflowCategory, len(cf.ByCategory))
		copy(cats, cf.ByCategory)
		sort.Slice(cats, func(i, j int) bool { return cats[i].Amount < cats[j].Amount })
		t := newTable(w, "TOP SPENDING CATEGORIES", "AMOUNT")
		n := 0
		for _, c := range cats {
			if c.Amount >= 0 || c.Category == nil {
				continue
			}
			t.row(truncate(c.Category.Name, 28), money(c.Amount))
			n++
			if n >= top {
				break
			}
		}
		t.flush()
		fmt.Fprintln(w)
	}

	if len(cf.ByMerchant) > 0 {
		merch := make([]*monarch.CashflowMerchant, len(cf.ByMerchant))
		copy(merch, cf.ByMerchant)
		sort.Slice(merch, func(i, j int) bool { return merch[i].Amount < merch[j].Amount })
		t := newTable(w, "TOP MERCHANTS", "AMOUNT")
		n := 0
		for _, m := range merch {
			if m.Amount >= 0 || m.Merchant == nil {
				continue
			}
			t.row(truncate(m.Merchant.Name, 28), money(m.Amount))
			n++
			if n >= top {
				break
			}
		}
		t.flush()
	}
}

func renderCategories(w io.Writer, cats []*monarch.Category) {
	sorted := make([]*monarch.Category, len(cats))
	copy(sorted, cats)
	sort.Slice(sorted, func(i, j int) bool {
		gi, gj := "", ""
		if sorted[i].Group != nil {
			gi = sorted[i].Group.Name
		}
		if sorted[j].Group != nil {
			gj = sorted[j].Group.Name
		}
		if gi != gj {
			return gi < gj
		}
		return sorted[i].Order < sorted[j].Order
	})

	t := newTable(w, "ID", "GROUP", "CATEGORY")
	for _, c := range sorted {
		if c.IsDisabled {
			continue
		}
		g := ""
		if c.Group != nil {
			g = c.Group.Name
		}
		t.row(c.ID, g, c.Name)
	}
	t.flush()
}

// monthlyEstimate normalizes recurring outflows to a per-month figure.
// Relies on Monarch's sign convention: outflows are negative.
func monthlyEstimate(recs []*monarch.RecurringItem) float64 {
	var monthly float64
	for _, r := range recs {
		if r.Amount >= 0 {
			continue
		}
		switch strings.ToLower(r.Frequency) {
		case "monthly":
			monthly += -r.Amount
		case "yearly", "annually":
			monthly += -r.Amount / 12
		case "weekly":
			monthly += -r.Amount * 52 / 12
		case "biweekly", "every_2_weeks":
			monthly += -r.Amount * 26 / 12
		case "quarterly":
			monthly += -r.Amount / 3
		}
	}
	return monthly
}

func renderRecurring(w io.Writer, recs []*monarch.RecurringItem) {
	sorted := make([]*monarch.RecurringItem, len(recs))
	copy(sorted, recs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Amount < sorted[j].Amount })
	t := newTable(w, "MERCHANT", "AMOUNT", "FREQUENCY", "NEXT DATE", "CATEGORY")
	for _, r := range sorted {
		name := ""
		if r.Merchant != nil {
			name = r.Merchant.Name
		}
		cat := ""
		if r.Category != nil {
			cat = r.Category.Name
		}
		t.row(truncate(name, 28), money(r.Amount), r.Frequency,
			r.NextDate.Format("2006-01-02"), truncate(cat, 22))
	}
	t.flush()
	fmt.Fprintf(w, "\nEstimated recurring spend: %s/month\n", money(monthlyEstimate(recs)))
}

// liabilityTypes classifies snapshot account types for net-worth math.
var liabilityTypes = map[string]bool{
	"credit":          true,
	"loan":            true,
	"other_liability": true,
}

type networthPoint struct {
	Period      string
	Assets      float64
	Liabilities float64
	NetWorth    float64
}

// networthPoints aggregates snapshots into per-period asset/liability/net
// rows. abs() keeps it correct under either sign convention Monarch returns
// for liability balances.
func networthPoints(snaps []*monarch.AccountSnapshot) []networthPoint {
	type row struct{ assets, liabilities float64 }
	byMonth := map[string]*row{}
	var months []string
	for _, sn := range snaps {
		r, ok := byMonth[sn.Month]
		if !ok {
			r = &row{}
			byMonth[sn.Month] = r
			months = append(months, sn.Month)
		}
		if liabilityTypes[sn.Type] {
			r.liabilities += sn.TotalValue
		} else {
			r.assets += sn.TotalValue
		}
	}
	sort.Strings(months)
	points := make([]networthPoint, 0, len(months))
	for _, m := range months {
		r := byMonth[m]
		points = append(points, networthPoint{
			Period:      m,
			Assets:      r.assets,
			Liabilities: abs(r.liabilities),
			NetWorth:    r.assets - abs(r.liabilities),
		})
	}
	return points
}

func renderNetworth(w io.Writer, snaps []*monarch.AccountSnapshot) {
	t := newTable(w, "PERIOD", "ASSETS", "LIABILITIES", "NET WORTH")
	for _, p := range networthPoints(snaps) {
		t.row(p.Period, money(p.Assets), money(p.Liabilities), money(p.NetWorth))
	}
	t.flush()
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
