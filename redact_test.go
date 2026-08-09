package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactErrCoversAllSecretShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		leak string
	}{
		{"header scheme", `request failed: Authorization: Token abc123SECRET dropped`, "abc123SECRET"},
		{"json body", `decode: {"token":"abc123SECRET"}`, "abc123SECRET"},
		{"single quotes", `config token='abc123SECRET' invalid`, "abc123SECRET"},
		{"bare bearer", `unexpected header Bearer abc123SECRETabcd`, "abc123SECRET"},
		{"bare jwt", `parse eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.sig failed`, "eyJhbGciOiJIUzI1NiI"},
		{"password", `login body {"password":"hunter2secret"}`, "hunter2secret"},
		{"api key", `api_key: abc123SECRET`, "abc123SECRET"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactErr(errors.New(c.in)).Error()
			if strings.Contains(got, c.leak) {
				t.Errorf("leak survived redaction: %q", got)
			}
		})
	}
}

func TestRedactErrLeavesPlainMessagesAlone(t *testing.T) {
	for _, plain := range []string{
		`invalid month "2026-13": use YYYY-MM`,
		"monarch: GetAccounts: HTTP 500",
		"rate limited: too many Monarch calls; wait a minute before retrying",
	} {
		if got := redactErr(errors.New(plain)).Error(); got != plain {
			t.Errorf("plain message mangled: %q -> %q", plain, got)
		}
	}
}
