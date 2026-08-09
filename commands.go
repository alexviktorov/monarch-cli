package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"monarch-cli/pkg/monarch"
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
	renderAccounts(os.Stdout, accounts, *all)
}

// ---- transactions ----

func cmdTransactions(args []string) {
	fs := flag.NewFlagSet("transactions", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	asCSV := fs.Bool("csv", false, "output CSV")
	start := fs.String("start", "", "start date YYYY-MM-DD")
	end := fs.String("end", "", "end date YYYY-MM-DD")
	limit := fs.Int("limit", 50, "max results per page")
	offset := fs.Int("offset", 0, "pagination offset")
	all := fs.Bool("all", false, "fetch every matching transaction (cap 10000)")
	search := fs.String("search", "", "search text")
	account := fs.String("account", "", "filter by account ID (see `monarch accounts`)")
	category := fs.String("category", "", "filter by category ID (see `monarch categories`)")
	tag := fs.String("tag", "", "filter by tag ID (see `monarch tags` output via --json)")
	txID := fs.String("id", "", "show full detail for one transaction ID")
	minAmt := fs.Float64("min", 0, "minimum absolute amount (client-side, post-pagination)")
	maxAmt := fs.Float64("max", 0, "maximum absolute amount (client-side, post-pagination)")
	hasNotes := fs.Bool("has-notes", false, "only transactions with notes")
	hasAttachments := fs.Bool("has-attachments", false, "only transactions with attachments")
	isSplit := fs.Bool("is-split", false, "only split transactions")
	isRecurring := fs.Bool("is-recurring", false, "only recurring transactions")
	hidden := fs.Bool("hidden", false, "only transactions hidden from reports")
	needsReview := fs.Bool("needs-review", false, "only transactions needing review")
	fs.Parse(args)

	if *asJSON && *asCSV {
		fatal("--json and --csv are mutually exclusive")
	}

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}

	if *txID != "" {
		detail, err := client.Transactions.Get(context.Background(), *txID)
		if err != nil {
			fatal("%v", err)
		}
		if *asJSON {
			printJSON(detail)
			return
		}
		renderTransactionDetail(os.Stdout, detail)
		return
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
	if *tag != "" {
		q = q.WithTags(*tag)
	}
	if *minAmt != 0 {
		q = q.WithMinAmount(*minAmt)
	}
	if *maxAmt != 0 {
		q = q.WithMaxAmount(*maxAmt)
	}
	if *hasNotes {
		q = q.WithHasNotes(true)
	}
	if *hasAttachments {
		q = q.WithHasAttachments(true)
	}
	if *isSplit {
		q = q.WithIsSplit(true)
	}
	if *isRecurring {
		q = q.WithIsRecurring(true)
	}
	if *hidden {
		q = q.WithHiddenFromReports(true)
	}
	if *needsReview {
		q = q.WithNeedsReview(true)
	}

	var list *monarch.TransactionList
	if *all {
		txs, err := q.All(context.Background(), 0)
		if err != nil {
			fatal("%v", err)
		}
		list = &monarch.TransactionList{Transactions: txs, TotalCount: len(txs), Fetched: len(txs)}
	} else {
		list, err = q.Execute(context.Background())
		if err != nil {
			fatal("%v", err)
		}
	}
	switch {
	case *asJSON:
		printJSON(list)
	case *asCSV:
		if err := csvTransactions(os.Stdout, list.Transactions); err != nil {
			fatal("%v", err)
		}
	default:
		renderTransactions(os.Stdout, list)
	}
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
	renderSummary(os.Stdout, s)
}

// ---- budget ----

func cmdBudget(args []string) {
	fs := flag.NewFlagSet("budget", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	asCSV := fs.Bool("csv", false, "output CSV")
	month := fs.String("month", "", "budget month YYYY-MM (default: current month)")
	fs.Parse(args)

	if *asJSON && *asCSV {
		fatal("--json and --csv are mutually exclusive")
	}

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
	switch {
	case *asJSON:
		printJSON(budgets)
	case *asCSV:
		if err := csvBudget(os.Stdout, budgets); err != nil {
			fatal("%v", err)
		}
	default:
		renderBudget(os.Stdout, budgets, start.Format("January 2006"))
	}
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
	cf, err := client.Cashflow.Get(context.Background(), monarch.CashflowParams{
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
	renderCashflow(os.Stdout, cf, s, e, *top)
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
	cats, err := client.Categories.List(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(cats)
		return
	}
	renderCategories(os.Stdout, cats)
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
	renderRecurring(os.Stdout, recs)
}

// ---- networth ----

func cmdNetworth(args []string) {
	fs := flag.NewFlagSet("networth", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	start := fs.String("start", "", "start date YYYY-MM-DD (default: 12 months ago)")
	timeframe := fs.String("timeframe", "month", "granularity: month or year")
	daily := fs.Bool("daily", false, "per-day aggregate balances instead of monthly by type")
	fs.Parse(args)

	s := time.Now().AddDate(-1, 0, 0)
	if *start != "" {
		s = parseDate(*start, "start")
	}

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	if *daily {
		snaps, err := client.Accounts.GetDailySnapshots(context.Background(), s, time.Time{}, "")
		if err != nil {
			fatal("%v", err)
		}
		if *asJSON {
			printJSON(snaps)
			return
		}
		renderDailyNetworth(os.Stdout, snaps)
		return
	}
	snaps, err := client.Accounts.GetSnapshots(context.Background(), monarch.SnapshotParams{
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
	renderNetworth(os.Stdout, snaps)
}

// ---- holdings / rules / goals / institutions ----

func cmdHoldings(args []string) {
	fs := flag.NewFlagSet("holdings", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	account := fs.String("account", "", "restrict to one account ID")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	holdings, err := client.Holdings.List(context.Background(), *account)
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(holdings)
		return
	}
	renderHoldings(os.Stdout, holdings)
}

func cmdRules(args []string) {
	fs := flag.NewFlagSet("rules", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	rules, err := client.Rules.List(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(rules)
		return
	}
	renderRules(os.Stdout, rules)
}

func cmdGoals(args []string) {
	fs := flag.NewFlagSet("goals", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	goals, err := client.Goals.List(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(goals)
		return
	}
	renderGoals(os.Stdout, goals)
}

func cmdInstitutions(args []string) {
	fs := flag.NewFlagSet("institutions", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	creds, err := client.Institutions.List(context.Background())
	if err != nil {
		fatal("%v", err)
	}
	if *asJSON {
		printJSON(creds)
		return
	}
	renderInstitutions(os.Stdout, creds)
}

// ---- overview ----

func cmdOverview(args []string) {
	fs := flag.NewFlagSet("overview", flag.ExitOnError)
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	ctx := context.Background()
	monthStart, _ := monthRange(time.Now())

	var (
		wg       sync.WaitGroup
		accounts []*monarch.Account
		summary  *monarch.CashflowSummary
		recent   *monarch.TransactionList
		errs     [3]error
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		accounts, errs[0] = client.Accounts.List(ctx)
	}()
	go func() {
		defer wg.Done()
		summary, errs[1] = client.Cashflow.GetSummary(ctx, monarch.CashflowParams{
			StartDate: monthStart, EndDate: time.Now(),
		})
	}()
	go func() {
		defer wg.Done()
		recent, errs[2] = client.Transactions.Query().Limit(10).Execute(ctx)
	}()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			fatal("%v", err)
		}
	}

	renderAccounts(os.Stdout, accounts, false)
	fmt.Fprintf(os.Stdout, "\nThis month so far: income %s, expenses %s, savings rate %.1f%%\n\n",
		money(summary.Income), money(summary.Expense), summary.SavingsRate*100)
	renderTransactions(os.Stdout, recent)
}
