package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"monarch-cli/pkg/monarch"
)

var update = flag.Bool("update", false, "rewrite golden files")

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run `go test -run %s -update`)", err, t.Name())
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func mustDate(t *testing.T, s string) monarch.Date {
	t.Helper()
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return monarch.Date{Time: tm}
}

func TestRenderAccounts(t *testing.T) {
	accounts := []*monarch.Account{
		{ID: "a1", DisplayName: "Everyday Checking", DisplayBalance: 5321.09,
			IncludeInNetWorth: true, DisplayLastUpdatedAt: mustDate(t, "2026-08-07"),
			Type: &monarch.AccountType{Name: "depository", Display: "Cash"}, Institution: &monarch.InstitutionRef{Name: "Chase"}},
		{ID: "a2", DisplayName: "Hidden Sock Drawer", DisplayBalance: 12, IsHidden: true,
			DisplayLastUpdatedAt: mustDate(t, "2026-01-01")},
		// Liability with a POSITIVE balance — the convention the live API
		// uses. Net worth must SUBTRACT it (5321.09 − 840.55 = 4480.54),
		// never add; the golden files pin that.
		{ID: "a3", DisplayName: "Visa", DisplayBalance: 840.55, IncludeInNetWorth: true,
			DisplayLastUpdatedAt: mustDate(t, "2026-08-06"),
			Type:                 &monarch.AccountType{Name: "credit", Display: "Credit"}, Institution: &monarch.InstitutionRef{Name: "Chase"}},
	}
	var visible, all bytes.Buffer
	renderAccounts(&visible, accounts, false)
	checkGolden(t, "accounts.txt", visible.Bytes())
	renderAccounts(&all, accounts, true)
	checkGolden(t, "accounts_all.txt", all.Bytes())
}

func TestRenderTransactions(t *testing.T) {
	list := &monarch.TransactionList{
		TotalCount: 132, HasMore: true, NextOffset: 2,
		Transactions: []*monarch.Transaction{
			{ID: "t1", Date: mustDate(t, "2026-08-01"), Amount: -52.5, PlaidName: "WHOLEFDS",
				Merchant: &monarch.MerchantRef{Name: "Whole Foods"},
				Category: &monarch.CategoryRef{Name: "Groceries"},
				Account:  &monarch.AccountRef{DisplayName: "Everyday Checking"}},
			{ID: "t2", Date: mustDate(t, "2026-08-02"), Amount: 2000, PlaidName: "ACME PAYROLL"},
		},
	}
	var buf bytes.Buffer
	renderTransactions(&buf, list)
	checkGolden(t, "transactions.txt", buf.Bytes())
}

func TestRenderSummary(t *testing.T) {
	var buf bytes.Buffer
	renderSummary(&buf, &monarch.TransactionSummary{
		Count: 4200, First: "2019-01-02", Last: "2026-08-01",
		SumIncome: 100000, SumExpense: -80000.5, Avg: -12.34, MaxExpense: -900,
	})
	checkGolden(t, "summary.txt", buf.Bytes())
}

func TestRenderBudget(t *testing.T) {
	rows := []*monarch.BudgetRow{
		{CategoryID: "c1", Category: &monarch.CategoryRef{ID: "c1", Name: "Groceries"},
			GroupType: "expense", Amount: 600, Spent: 420.5, Remaining: 179.5},
		{CategoryID: "c2", Amount: 100, Spent: 0, Remaining: 100},
		// Income rows are excluded from the table and totals — the golden
		// file proves it: this row must leave the output unchanged.
		{CategoryID: "c9", Category: &monarch.CategoryRef{ID: "c9", Name: "Paycheck"},
			GroupType: "income", Amount: 5000, Spent: -5000, Remaining: 0},
	}
	var buf bytes.Buffer
	renderBudget(&buf, rows, "August 2026")
	checkGolden(t, "budget.txt", buf.Bytes())
}

func TestRenderCashflow(t *testing.T) {
	cf := &monarch.Cashflow{
		Summary: &monarch.CashflowSummary{Income: 8000, Expense: 5000, Savings: 3000, SavingsRate: 0.375},
		ByCategory: []*monarch.CashflowCategory{
			{Category: &monarch.CategoryRef{Name: "Rent"}, Amount: -2000},
			{Category: &monarch.CategoryRef{Name: "Groceries"}, Amount: -800},
			{Category: &monarch.CategoryRef{Name: "Salary"}, Amount: 8000},
		},
		ByMerchant: []*monarch.CashflowMerchant{
			{Merchant: &monarch.MerchantRef{Name: "Landlord"}, Amount: -2000},
			{Merchant: &monarch.MerchantRef{Name: "Whole Foods"}, Amount: -800},
		},
	}
	var buf bytes.Buffer
	renderCashflow(&buf, cf, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), 10)
	checkGolden(t, "cashflow.txt", buf.Bytes())
}

func TestRenderCategories(t *testing.T) {
	cats := []*monarch.Category{
		{ID: "c2", Name: "Restaurants", Order: 2, Group: &monarch.CategoryGroup{Name: "Food"}},
		{ID: "c1", Name: "Groceries", Order: 1, Group: &monarch.CategoryGroup{Name: "Food"}},
		{ID: "c3", Name: "Old Thing", IsDisabled: true},
		{ID: "c4", Name: "Rent", Order: 1, Group: &monarch.CategoryGroup{Name: "Housing"}},
	}
	var buf bytes.Buffer
	renderCategories(&buf, cats)
	checkGolden(t, "categories.txt", buf.Bytes())
}

func TestRenderRecurring(t *testing.T) {
	recs := []*monarch.RecurringItem{
		{Merchant: &monarch.MerchantRef{Name: "Netflix"}, Amount: -15.99, Frequency: "monthly",
			NextDate: mustDate(t, "2026-08-15"), Category: &monarch.CategoryRef{Name: "Streaming"}},
		{Merchant: &monarch.MerchantRef{Name: "Insurance Co"}, Amount: -1200, Frequency: "yearly",
			NextDate: mustDate(t, "2026-09-01"), Category: &monarch.CategoryRef{Name: "Insurance"}},
		{Merchant: &monarch.MerchantRef{Name: "Paycheck"}, Amount: 5000, Frequency: "biweekly",
			NextDate: mustDate(t, "2026-08-14")},
	}
	var buf bytes.Buffer
	renderRecurring(&buf, recs)
	checkGolden(t, "recurring.txt", buf.Bytes())
}

func TestMonthlyEstimate(t *testing.T) {
	recs := []*monarch.RecurringItem{
		{Amount: -12, Frequency: "monthly"},
		{Amount: -120, Frequency: "yearly"},
		{Amount: -120, Frequency: "annually"},
		{Amount: -12, Frequency: "weekly"},
		{Amount: -12, Frequency: "biweekly"},
		{Amount: -30, Frequency: "quarterly"},
		{Amount: -99, Frequency: "one_time"}, // unknown frequency ignored
		{Amount: 5000, Frequency: "monthly"}, // income ignored
	}
	got := monthlyEstimate(recs)
	want := 12.0 + 10 + 10 + 52.0 + 26.0 + 10
	if got != want {
		t.Errorf("monthlyEstimate = %v, want %v", got, want)
	}
}

func TestRenderNetworth(t *testing.T) {
	snaps := []*monarch.AccountSnapshot{
		{Month: "2026-07", Type: "depository", TotalValue: 10000},
		{Month: "2026-07", Type: "brokerage", TotalValue: 25000},
		{Month: "2026-07", Type: "credit", TotalValue: -1500},
		{Month: "2026-06", Type: "depository", TotalValue: 9000},
		{Month: "2026-06", Type: "loan", TotalValue: 5000}, // liability, positive sign convention
	}
	var buf bytes.Buffer
	renderNetworth(&buf, snaps)
	checkGolden(t, "networth.txt", buf.Bytes())
}

// ---- hostile remote text (CWE-150) ----

type renderCase struct {
	name   string
	render func(w io.Writer)
}

// renderCases builds one fixture per human-readable renderer with s in every
// remote-origin string field it prints.
func renderCases(t *testing.T, s string) []renderCase {
	t.Helper()
	day := mustDate(t, "2026-08-01")
	cat := &monarch.CategoryRef{ID: s, Name: s}
	merch := &monarch.MerchantRef{ID: s, Name: s}
	acct := &monarch.AccountRef{ID: s, DisplayName: s}
	tags := []*monarch.Tag{{ID: s, Name: s}, {ID: s, Name: s}}
	full := monarch.Transaction{ID: s, Date: day, Amount: -5, PlaidName: s, Notes: s,
		Merchant: merch, Category: cat, Account: acct, Tags: tags}
	// No merchant: the raw statement text stands in for it.
	bare := monarch.Transaction{ID: s, Date: day, Amount: -5, PlaidName: s}
	hide := true
	return []renderCase{
		{"accounts", func(w io.Writer) {
			renderAccounts(w, []*monarch.Account{{ID: s, DisplayName: s, DisplayLastUpdatedAt: day,
				Type:        &monarch.AccountType{Name: s, Display: s},
				Institution: &monarch.InstitutionRef{Name: s}}}, true)
		}},
		{"transactions", func(w io.Writer) {
			renderTransactions(w, &monarch.TransactionList{TotalCount: 2,
				Transactions: []*monarch.Transaction{&full, &bare}})
		}},
		{"summary", func(w io.Writer) {
			renderSummary(w, &monarch.TransactionSummary{Count: 1, First: s, Last: s})
		}},
		{"budget", func(w io.Writer) {
			renderBudget(w, []*monarch.BudgetRow{
				{CategoryID: s, Category: cat, GroupType: "expense", Month: s, Amount: 1},
				{CategoryID: s, GroupType: "expense", Month: s, Amount: 1}, // no category: the raw id is shown
			}, "August 2026")
		}},
		{"cashflow", func(w io.Writer) {
			renderCashflow(w, &monarch.Cashflow{
				ByCategory: []*monarch.CashflowCategory{{Category: cat, Amount: -1}},
				ByMerchant: []*monarch.CashflowMerchant{{Merchant: merch, Amount: -1}},
			}, day.Time, day.Time, 10)
		}},
		{"categories", func(w io.Writer) {
			renderCategories(w, []*monarch.Category{{ID: s, Name: s,
				Group: &monarch.CategoryGroup{ID: s, Name: s, Type: s}}})
		}},
		{"recurring", func(w io.Writer) {
			renderRecurring(w, []*monarch.RecurringItem{{Merchant: merch, Amount: -1,
				Frequency: s, NextDate: day, Category: cat}})
		}},
		{"networth", func(w io.Writer) {
			renderNetworth(w, []*monarch.AccountSnapshot{{Month: s, Type: s, TotalValue: 1}})
		}},
		{"transaction detail", func(w io.Writer) {
			renderTransactionDetail(w, &monarch.TransactionDetail{Transaction: full,
				Splits: []*monarch.TransactionSplit{{ID: s, Amount: -1, Notes: s, Merchant: merch, Category: cat}}})
		}},
		{"transaction detail without merchant", func(w io.Writer) {
			renderTransactionDetail(w, &monarch.TransactionDetail{Transaction: bare})
		}},
		{"holdings", func(w io.Writer) {
			renderHoldings(w, []*monarch.Holding{{ID: s, Quantity: 1,
				Security: &monarch.Security{ID: s, Name: s, Type: s, Ticker: s}}})
		}},
		{"rules", func(w io.Writer) {
			renderRules(w, []*monarch.Rule{{ID: s,
				// Not valid JSON, so the raw bytes are what gets displayed.
				MerchantCriteria: json.RawMessage(s), AmountCriteria: json.RawMessage(s),
				CategoryIDs: []string{s}, SetCategoryAction: cat, SetMerchantAction: merch,
				AddTagsAction: tags, SetHideFromReportsAction: &hide,
				ReviewStatusAction: s, LastAppliedAt: s}})
		}},
		{"goals", func(w io.Writer) {
			renderGoals(w, []*monarch.Goal{{ID: s, Type: s, Name: s, Status: s,
				TargetDate: s, ForecastedCompletionDate: s}})
		}},
		{"institutions", func(w io.Writer) {
			renderInstitutions(w, []*monarch.Credential{{ID: s, DataProvider: s,
				Institution: &monarch.Institution{ID: s, Name: s, URL: s}}})
		}},
	}
}

// Whatever a remote field holds, a renderer may hand the terminal nothing but
// printable text and its own line breaks — and exactly as many lines as the
// same data produces with harmless text, so no row can be forged or hidden.
func TestRenderersNeutralizeHostileRemoteText(t *testing.T) {
	hostile := "evil\x1b[2J\r\n2026-01-01\tFORGED\a" + c1CSI + bidiOverride +
		zeroWidthSpace + lineSeparator + "\xff end"
	benign := renderCases(t, "x")
	for i, c := range renderCases(t, hostile) {
		t.Run(c.name, func(t *testing.T) {
			var got, ref bytes.Buffer
			c.render(&got)
			benign[i].render(&ref)
			out := got.String()

			if !utf8.ValidString(out) {
				t.Errorf("output is not valid UTF-8: %q", out)
			}
			for _, r := range out {
				if r == '\n' { // the renderer's own line breaks
					continue
				}
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 {
					t.Errorf("raw %U reached the output: %q", r, out)
					break
				}
			}
			if !strings.Contains(out, `evil\x1b[2J`) {
				t.Errorf("hostile text is not shown as a visible escape: %q", out)
			}
			if g, w := strings.Count(out, "\n"), strings.Count(ref.String(), "\n"); g != w {
				t.Errorf("line count = %d, want %d (as with harmless text): %q", g, w, out)
			}
		})
	}
}

func TestRenderTransactionDetailEscapesRemoteText(t *testing.T) {
	d := &monarch.TransactionDetail{
		Transaction: monarch.Transaction{
			ID: "t1", Date: mustDate(t, "2026-08-01"), Amount: -52.5,
			PlaidName: "WHOLEFDS\x1b[2J",
			Notes:     "line one\nTransaction t2\x1b[1A",
			Merchant:  &monarch.MerchantRef{Name: "Whole" + bidiOverride + "Foods"},
			Category:  &monarch.CategoryRef{ID: "c1\r", Name: "Groceries\t"},
			Account:   &monarch.AccountRef{DisplayName: "Checking\r"},
			Tags:      []*monarch.Tag{{Name: "a\a"}, {Name: "b"}},
		},
		Splits: []*monarch.TransactionSplit{{ID: "s1\n", Amount: -10,
			Merchant: &monarch.MerchantRef{Name: "M\x1b"}, Category: &monarch.CategoryRef{Name: "C\x1b"}}},
	}
	var buf bytes.Buffer
	renderTransactionDetail(&buf, d)
	want := `Transaction t1
  Date:      2026-08-01
  Amount:    -$52.50
  Merchant:  Whole\u202eFoods
  Statement: WHOLEFDS\x1b[2J
  Category:  Groceries\t (c1\r)
  Account:   Checking\r
  Notes:     line one\nTransaction t2\x1b[1A
  Tags:      a\a, b
  Splits:
    s1\n  -$10.00  M\x1b  C\x1b
`
	if got := buf.String(); got != want {
		t.Errorf("output mismatch\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}
