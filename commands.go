package main

import (
	"context"
	"flag"
	"os"
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
	start := fs.String("start", "", "start date YYYY-MM-DD")
	end := fs.String("end", "", "end date YYYY-MM-DD")
	limit := fs.Int("limit", 50, "max results")
	offset := fs.Int("offset", 0, "pagination offset")
	search := fs.String("search", "", "search text")
	account := fs.String("account", "", "filter by account ID (see `monarch accounts`)")
	category := fs.String("category", "", "filter by category ID (see `monarch categories`)")
	minAmt := fs.Float64("min", 0, "minimum absolute amount")
	maxAmt := fs.Float64("max", 0, "maximum absolute amount")
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
	renderTransactions(os.Stdout, list)
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
	renderBudget(os.Stdout, budgets, start.Format("January 2006"))
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
	fs.Parse(args)

	s := time.Now().AddDate(-1, 0, 0)
	if *start != "" {
		s = parseDate(*start, "start")
	}

	client, err := newClient()
	if err != nil {
		fatal("%v", err)
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
