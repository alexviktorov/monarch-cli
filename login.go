package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func readLine(prompt string) string {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

func readPassword(prompt string) string {
	fmt.Print(prompt)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		// Not a TTY (e.g. piped input) — fall back to plain read.
		return readLine("")
	}
	return strings.TrimSpace(string(b))
}

func cmdLogin(args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	email := fs.String("email", "", "Monarch account email (prompted if omitted)")
	totpSecret := fs.String("totp-secret", "", "TOTP secret key — generates the 2FA code automatically (optional)")
	fs.Parse(args)

	ctx := context.Background()
	client, err := newClientForLogin()
	if err != nil {
		fatal("%v", err)
	}

	e := *email
	if e == "" {
		e = readLine("Email: ")
	}
	password := readPassword("Password: ")

	if *totpSecret != "" {
		err = client.Auth.LoginWithTOTP(ctx, e, password, *totpSecret)
	} else {
		err = client.Auth.Login(ctx, e, password)
		if err != nil {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "MFA required"):
				code := readLine("Two-factor code: ")
				err = client.Auth.LoginWithMFA(ctx, e, password, code)
			case strings.Contains(msg, "Email OTP required"):
				code := readLine("Code sent to your email: ")
				err = client.Auth.LoginWithEmailOTP(ctx, e, password, code)
			}
		}
	}
	if err != nil {
		fatal("login failed: %v", err)
	}

	// The client saves the session automatically (SessionFile option), but
	// save explicitly too so MFA paths are covered regardless of library version.
	if err := client.Auth.SaveSession(sessionPath()); err != nil {
		fatal("logged in, but failed to save session: %v", err)
	}
	_ = os.Chmod(sessionPath(), 0o600)
	fmt.Printf("Logged in as %s. Session saved to %s\n", e, sessionPath())
}

func cmdLogout(args []string) {
	sp := sessionPath()
	if err := os.Remove(sp); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No saved session.")
			return
		}
		fatal("%v", err)
	}
	fmt.Printf("Session removed (%s).\n", sp)
}

func cmdWhoami(args []string) {
	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	s := client.GetSession()
	if s == nil || s.Token == "" {
		fatal("not logged in")
	}
	if s.Email != "" {
		fmt.Printf("Logged in as %s\n", s.Email)
	} else {
		fmt.Println("Authenticated via token")
	}
	if !s.ExpiresAt.IsZero() {
		fmt.Printf("Session expires: %s\n", s.ExpiresAt.Format("2006-01-02 15:04 MST"))
	}
}
