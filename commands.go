package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshaffer321/monarch-go/v2/pkg/monarch"
)

func parseDate(s, name string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		fatal("invalid --%s %q: use YYYY-MM-DD", name, s)
	}
	return t
}

func monthRange(now time.Time) (time.Time, time.Time) {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

// ---- accounts ----

func cmdAccounts(args []string) {
	fs := flag.NewFlagSet("accounts", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	all := fs.Bool("all", false, "include hidden accounts")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	accounts, err := client.Accounts.List(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(accounts)
		return
	}

	t := newTable("ID", "NAME", "TYPE", "INSTITUTION", "BALANCE", "UPDATED")
	var total float64
	for _, a := range accounts {
		if a.IsHidden && !*all {
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
	fmt.Printf("\nNet worth (included accounts): %s\n", money(total))
}

// ---- transactions ----

func cmdTransactions(args []string) {
	fs := flag.NewFlagSet("transactions", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	start := fs.String("start", "", "start date YYYY-MM-DD")
	end := fs.String("end", "", "end date YYYY-MM-DD")
	limit := fs.Int("limit", 50, "max results")
	offset := fs.Int("offset", 0, "pagination offset")
	search := fs.String("search", "", "search text")
	account := fs.String("account", "", "filter by account ID (see `monarch accounts`)")
	category := fs.String("category", "", "filter by category ID (see `monarch categories`)")
	minAmt := fs.Float64("min", 0, "minimum amount")
	maxAmt := fs.Float64("max", 0, "maximum amount")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}

	q := client.Transactions.Query().Limit(*limit).Offset(*offset)
	if *start != "" || *end != "" {
		s, e := *start, *end
		if s == "" {
			s = "1970-01-01"
		}
		if e == "" {
			e = time.Now().Format("2006-01-02")
		}
		q = q.Between(parseDate(s, "start"), parseDate(e, "end"))
	}
	if *search != "" {
		q = q.Search(*search)
	}
	if *account != "" {
		q = q.WithAccounts(*account)
	}
	if *category != "" {
		q = q.WithCategories(*category)
	}
	if *minAmt != 0 {
		q = q.WithMinAmount(*minAmt)
	}
	if *maxAmt != 0 {
		q = q.WithMaxAmount(*maxAmt)
	}

	list, err := q.Execute(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(list)
		return
	}

	t := newTable("DATE", "MERCHANT", "CATEGORY", "ACCOUNT", "AMOUNT")
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
	fmt.Printf("\nShowing %d of %d transactions", len(list.Transactions), list.TotalCount)
	if list.HasMore {
		fmt.Printf(" (next: --offset %d)", list.NextOffset)
	}
	fmt.Println()
}

// ---- summary ----

func cmdSummary(args []string) {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	s, err := client.Transactions.GetSummary(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(s)
		return
	}
	fmt.Printf("Transactions: %d (%s to %s)\n", s.Count, s.First, s.Last)
	fmt.Printf("Total income:  %s\n", money(s.SumIncome))
	fmt.Printf("Total expense: %s\n", money(s.SumExpense))
	fmt.Printf("Average:       %s   Largest expense: %s\n", money(s.Avg), money(s.MaxExpense))
}

// ---- budget ----

func cmdBudget(args []string) {
	fs := flag.NewFlagSet("budget", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	month := fs.String("month", "", "budget month YYYY-MM (default: current month)")
	fs.Parse(args)

	now := time.Now()
	if *month != "" {
		m, err := time.Parse("2006-01", *month)
		if err != nil {
			fatal("invalid --month %q: use YYYY-MM", *month)
		}
		now = m
	}
	start, end := monthRange(now)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	budgets, err := client.Budgets.List(context.Background(), start, end)
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(budgets)
		return
	}

	t := newTable("CATEGORY", "BUDGET", "SPENT", "REMAINING")
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
	fmt.Printf("\nBudget for %s\n", start.Format("January 2006"))
}

// ---- cashflow ----

func cmdCashflow(args []string) {
	fs := flag.NewFlagSet("cashflow", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	start := fs.String("start", "", "start date YYYY-MM-DD (default: first of current month)")
	end := fs.String("end", "", "end date YYYY-MM-DD (default: today)")
	top := fs.Int("top", 10, "how many top categories/merchants to show")
	fs.Parse(args)

	s, e := monthRange(time.Now())
	if *start != "" {
		s = parseDate(*start, "start")
	}
	if *end != "" {
		e = parseDate(*end, "end")
	} else {
		e = time.Now()
	}

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	cf, err := client.Cashflow.Get(context.Background(), &monarch.CashflowParams{
		StartDate: s,
		EndDate:   e,
	})
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(cf)
		return
	}

	fmt.Printf("Cashflow %s → %s\n\n", s.Format("2006-01-02"), e.Format("2006-01-02"))
	if cf.Summary != nil {
		fmt.Printf("Income:       %s\n", money(cf.Summary.Income))
		fmt.Printf("Expenses:     %s\n", money(cf.Summary.Expense))
		fmt.Printf("Savings:      %s (%.1f%%)\n\n", money(cf.Summary.Savings), cf.Summary.SavingsRate*100)
	}

	if len(cf.ByCategory) > 0 {
		cats := make([]*monarch.CashflowCategory, len(cf.ByCategory))
		copy(cats, cf.ByCategory)
		sort.Slice(cats, func(i, j int) bool { return cats[i].Amount < cats[j].Amount })
		t := newTable("TOP SPENDING CATEGORIES", "AMOUNT")
		n := 0
		for _, c := range cats {
			if c.Amount >= 0 || c.Category == nil {
				continue
			}
			t.row(truncate(c.Category.Name, 28), money(c.Amount))
			n++
			if n >= *top {
				break
			}
		}
		t.flush()
		fmt.Println()
	}

	if len(cf.ByMerchant) > 0 {
		merch := make([]*monarch.CashflowMerchant, len(cf.ByMerchant))
		copy(merch, cf.ByMerchant)
		sort.Slice(merch, func(i, j int) bool { return merch[i].Amount < merch[j].Amount })
		t := newTable("TOP MERCHANTS", "AMOUNT")
		n := 0
		for _, m := range merch {
			if m.Amount >= 0 || m.Merchant == nil {
				continue
			}
			t.row(truncate(m.Merchant.Name, 28), money(m.Amount))
			n++
			if n >= *top {
				break
			}
		}
		t.flush()
	}
}

// ---- categories ----

func cmdCategories(args []string) {
	fs := flag.NewFlagSet("categories", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	cats, err := client.Transactions.Categories().List(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(cats)
		return
	}

	sort.Slice(cats, func(i, j int) bool {
		gi, gj := "", ""
		if cats[i].Group != nil {
			gi = cats[i].Group.Name
		}
		if cats[j].Group != nil {
			gj = cats[j].Group.Name
		}
		if gi != gj {
			return gi < gj
		}
		return cats[i].Order < cats[j].Order
	})

	t := newTable("ID", "GROUP", "CATEGORY")
	for _, c := range cats {
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

// ---- recurring ----

func cmdRecurring(args []string) {
	fs := flag.NewFlagSet("recurring", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	recs, err := client.Recurring.List(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(recs)
		return
	}

	sort.Slice(recs, func(i, j int) bool { return recs[i].Amount < recs[j].Amount })
	t := newTable("MERCHANT", "AMOUNT", "FREQUENCY", "NEXT DATE", "CATEGORY")
	var monthly float64
	for _, r := range recs {
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
		if r.Amount < 0 {
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
	}
	t.flush()
	fmt.Printf("\nEstimated recurring spend: %s/month\n", money(monthly))
}

// ---- networth ----

var liabilityTypes = map[string]bool{
	"credit":          true,
	"loan":            true,
	"other_liability": true,
}

func cmdNetworth(args []string) {
	fs := flag.NewFlagSet("networth", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	start := fs.String("start", "", "start date YYYY-MM-DD (default: 12 months ago)")
	timeframe := fs.String("timeframe", "month", "granularity: month or year")
	fs.Parse(args)

	s := time.Now().AddDate(-1, 0, 0)
	if *start != "" {
		s = parseDate(*start, "start")
	}

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	snaps, err := client.Accounts.GetSnapshots(context.Background(), &monarch.SnapshotParams{
		StartDate: s,
		Timeframe: *timeframe,
	})
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(snaps)
		return
	}

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

	t := newTable("PERIOD", "ASSETS", "LIABILITIES", "NET WORTH")
	for _, m := range months {
		r := byMonth[m]
		net := r.assets - abs(r.liabilities)
		t.row(m, money(r.assets), money(abs(r.liabilities)), money(net))
	}
	t.flush()
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
