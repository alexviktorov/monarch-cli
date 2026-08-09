package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

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
			Type: &monarch.AccountType{Display: "Cash"}, Institution: &monarch.InstitutionRef{Name: "Chase"}},
		{ID: "a2", DisplayName: "Hidden Sock Drawer", DisplayBalance: 12, IsHidden: true,
			DisplayLastUpdatedAt: mustDate(t, "2026-01-01")},
		{ID: "a3", DisplayName: "Visa", DisplayBalance: -840.55, IncludeInNetWorth: true,
			DisplayLastUpdatedAt: mustDate(t, "2026-08-06"),
			Type:                 &monarch.AccountType{Display: "Credit"}, Institution: &monarch.InstitutionRef{Name: "Chase"}},
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
