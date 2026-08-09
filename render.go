package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"monarch-cli/pkg/monarch"
)

// Pure rendering: every function writes a finished table/report to w and
// touches nothing else. commands.go supplies the data; golden tests in
// render_test.go pin the output.

// accountIsLiability classifies an account by its type name using the same
// set the net-worth snapshots use.
func accountIsLiability(a *monarch.Account) bool {
	return a.Type != nil && liabilityTypes[a.Type.Name]
}

// accountNetWorth splits included accounts into assets and liabilities.
// Monarch reports liability balances as positive magnitudes, so they must
// be SUBTRACTED, never summed; abs() keeps this correct under either sign
// convention (same trick as networthPoints).
func accountNetWorth(accounts []*monarch.Account) (assets, liabilities float64) {
	for _, a := range accounts {
		if !a.IncludeInNetWorth {
			continue
		}
		if accountIsLiability(a) {
			liabilities += abs(a.DisplayBalance)
		} else {
			assets += a.DisplayBalance
		}
	}
	return assets, liabilities
}

func renderAccounts(w io.Writer, accounts []*monarch.Account, all bool) {
	t := newTable(w, "ID", "NAME", "TYPE", "INSTITUTION", "BALANCE", "UPDATED")
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
		t.row(a.ID, truncate(a.DisplayName, 32), typ, truncate(inst, 24),
			money(a.DisplayBalance), a.DisplayLastUpdatedAt.Format("2006-01-02"))
	}
	t.flush()
	assets, liabilities := accountNetWorth(accounts)
	fmt.Fprintf(w, "\nNet worth (included accounts): %s  (assets %s − liabilities %s)\n",
		money(assets-liabilities), money(assets), money(liabilities))
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
		// Income rows would corrupt the spend totals (their actualAmount
		// is money received); the budget table is about spending.
		if b.GroupType == "income" {
			continue
		}
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

// ---- CSV output ----

func txMerchant(tx *monarch.Transaction) string {
	if tx.Merchant != nil && tx.Merchant.Name != "" {
		return tx.Merchant.Name
	}
	return tx.PlaidName
}

func csvTransactions(w io.Writer, txs []*monarch.Transaction) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"id", "date", "merchant", "category", "account", "amount", "notes", "tags"}); err != nil {
		return err
	}
	for _, tx := range txs {
		cat, acct := "", ""
		if tx.Category != nil {
			cat = tx.Category.Name
		}
		if tx.Account != nil {
			acct = tx.Account.DisplayName
		}
		var tags []string
		for _, tag := range tx.Tags {
			tags = append(tags, tag.Name)
		}
		if err := cw.Write([]string{
			tx.ID, tx.Date.Format("2006-01-02"), txMerchant(tx), cat, acct,
			strconv.FormatFloat(tx.Amount, 'f', 2, 64), tx.Notes, strings.Join(tags, ";"),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func csvBudget(w io.Writer, rows []*monarch.BudgetRow) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"category_id", "category", "group_type", "month", "budgeted", "spent", "remaining"}); err != nil {
		return err
	}
	for _, b := range rows {
		name := b.CategoryID
		if b.Category != nil {
			name = b.Category.Name
		}
		if err := cw.Write([]string{
			b.CategoryID, name, b.GroupType, b.Month,
			strconv.FormatFloat(b.Amount, 'f', 2, 64),
			strconv.FormatFloat(b.Spent, 'f', 2, 64),
			strconv.FormatFloat(b.Remaining, 'f', 2, 64),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ---- new read domains ----

func renderTransactionDetail(w io.Writer, d *monarch.TransactionDetail) {
	fmt.Fprintf(w, "Transaction %s\n", d.ID)
	fmt.Fprintf(w, "  Date:      %s\n", d.Date.Format("2006-01-02"))
	fmt.Fprintf(w, "  Amount:    %s\n", money(d.Amount))
	fmt.Fprintf(w, "  Merchant:  %s\n", txMerchant(&d.Transaction))
	if d.PlaidName != "" {
		fmt.Fprintf(w, "  Statement: %s\n", d.PlaidName)
	}
	if d.Category != nil {
		fmt.Fprintf(w, "  Category:  %s (%s)\n", d.Category.Name, d.Category.ID)
	}
	if d.Account != nil {
		fmt.Fprintf(w, "  Account:   %s\n", d.Account.DisplayName)
	}
	if d.Notes != "" {
		fmt.Fprintf(w, "  Notes:     %s\n", d.Notes)
	}
	if len(d.Tags) > 0 {
		var names []string
		for _, t := range d.Tags {
			names = append(names, t.Name)
		}
		fmt.Fprintf(w, "  Tags:      %s\n", strings.Join(names, ", "))
	}
	var flags []string
	for _, f := range []struct {
		on   bool
		name string
	}{
		{d.Pending, "pending"}, {d.HideFromReports, "hidden-from-reports"},
		{d.IsRecurring, "recurring"}, {d.NeedsReview, "needs-review"},
	} {
		if f.on {
			flags = append(flags, f.name)
		}
	}
	if len(flags) > 0 {
		fmt.Fprintf(w, "  Flags:     %s\n", strings.Join(flags, ", "))
	}
	if len(d.Splits) > 0 {
		fmt.Fprintln(w, "  Splits:")
		for _, sp := range d.Splits {
			name := ""
			if sp.Merchant != nil {
				name = sp.Merchant.Name
			}
			cat := ""
			if sp.Category != nil {
				cat = sp.Category.Name
			}
			fmt.Fprintf(w, "    %s  %s  %s  %s\n", sp.ID, money(sp.Amount), truncate(name, 24), truncate(cat, 20))
		}
	}
}

func renderHoldings(w io.Writer, holdings []*monarch.Holding) {
	sorted := make([]*monarch.Holding, len(holdings))
	copy(sorted, holdings)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TotalValue > sorted[j].TotalValue })
	t := newTable(w, "TICKER", "NAME", "QUANTITY", "PRICE", "VALUE", "BASIS")
	var total float64
	for _, h := range sorted {
		ticker, name, price := "", "", 0.0
		if h.Security != nil {
			ticker, name, price = h.Security.Ticker, h.Security.Name, h.Security.CurrentPrice
		}
		total += h.TotalValue
		t.row(ticker, truncate(name, 30), strconv.FormatFloat(h.Quantity, 'f', -1, 64),
			money(price), money(h.TotalValue), money(h.Basis))
	}
	t.flush()
	fmt.Fprintf(w, "\nTotal holdings value: %s\n", money(total))
}

func renderRules(w io.Writer, rules []*monarch.Rule) {
	sorted := make([]*monarch.Rule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Order < sorted[j].Order })
	t := newTable(w, "ID", "CRITERIA", "ACTION", "APPLIED(30D)", "LAST APPLIED")
	for _, r := range sorted {
		var criteria []string
		if len(r.MerchantCriteria) > 0 && string(r.MerchantCriteria) != "null" {
			criteria = append(criteria, "merchant:"+compactJSON(r.MerchantCriteria))
		}
		if len(r.AmountCriteria) > 0 && string(r.AmountCriteria) != "null" {
			criteria = append(criteria, "amount:"+compactJSON(r.AmountCriteria))
		}
		if len(r.CategoryIDs) > 0 {
			criteria = append(criteria, fmt.Sprintf("categories:%d", len(r.CategoryIDs)))
		}
		var actions []string
		if r.SetCategoryAction != nil {
			actions = append(actions, "category→"+r.SetCategoryAction.Name)
		}
		if r.SetMerchantAction != nil {
			actions = append(actions, "merchant→"+r.SetMerchantAction.Name)
		}
		if len(r.AddTagsAction) > 0 {
			var names []string
			for _, tag := range r.AddTagsAction {
				names = append(names, tag.Name)
			}
			actions = append(actions, "tags+"+strings.Join(names, "/"))
		}
		if r.SetHideFromReportsAction != nil && *r.SetHideFromReportsAction {
			actions = append(actions, "hide")
		}
		if r.ReviewStatusAction != "" {
			actions = append(actions, "review→"+r.ReviewStatusAction)
		}
		last := r.LastAppliedAt
		if len(last) > 10 {
			last = last[:10]
		}
		t.row(r.ID, truncate(strings.Join(criteria, " "), 40),
			truncate(strings.Join(actions, ", "), 36),
			strconv.Itoa(r.RecentApplicationCount), last)
	}
	t.flush()
}

func compactJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

func renderGoals(w io.Writer, goals []*monarch.Goal) {
	t := newTable(w, "NAME", "STATUS", "BALANCE", "TARGET", "PROGRESS", "EST. DONE")
	for _, g := range goals {
		target := ""
		if g.TargetAmount != 0 {
			target = money(g.TargetAmount)
		}
		done := g.ForecastedCompletionDate
		if len(done) > 10 {
			done = done[:10]
		}
		t.row(truncate(g.Name, 28), g.Status, money(g.CurrentBalance), target,
			fmt.Sprintf("%.0f%%", g.Progress*100), done)
	}
	t.flush()
}

func renderInstitutions(w io.Writer, creds []*monarch.Credential) {
	t := newTable(w, "INSTITUTION", "PROVIDER", "STATUS")
	broken := 0
	for _, c := range creds {
		name := ""
		if c.Institution != nil {
			name = c.Institution.Name
		}
		status := "ok"
		if c.UpdateRequired {
			status = "UPDATE REQUIRED"
			broken++
		}
		t.row(truncate(name, 32), c.DataProvider, status)
	}
	t.flush()
	if broken > 0 {
		fmt.Fprintf(w, "\n%d connection(s) need attention — fix at app.monarch.com → Settings → Accounts\n", broken)
	}
}

func renderDailyNetworth(w io.Writer, snaps []*monarch.DailySnapshot) {
	t := newTable(w, "DATE", "BALANCE")
	for _, s := range snaps {
		t.row(s.Date.Format("2006-01-02"), money(s.Balance))
	}
	t.flush()
}
