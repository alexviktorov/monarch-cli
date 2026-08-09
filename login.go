package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"monarch-cli/pkg/monarch"
)

// stdinReader is shared across all prompts: a fresh bufio.Reader per call
// would discard lines it had buffered but not returned, silently breaking
// piped/scripted logins (bufio reads up to 4KB at a time, not one line).
var stdinReader = bufio.NewReader(os.Stdin)

func readLine(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(line)
}

// readSecret prompts without echo. It refuses to run without a TTY so a
// secret is never silently read (and echoed) from a pipe; scripted logins
// must opt in with --password-stdin.
func readSecret(prompt string) (string, error) {
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", errors.New("stdin is not a terminal; use --password-stdin for scripted logins")
	}
	fmt.Print(prompt)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func cmdLogin(args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	email := fs.String("email", "", "Monarch account email (prompted if omitted)")
	useTOTP := fs.Bool("totp", false, "generate the 2FA code from a TOTP secret (read from MONARCH_TOTP_SECRET or prompted — never from argv)")
	passwordStdin := fs.Bool("password-stdin", false, "read the password from the first line of stdin (for scripts)")
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
	var password string
	if *passwordStdin {
		password = readLine("")
	} else {
		password, err = readSecret("Password: ")
		if err != nil {
			fatal("%v", err)
		}
	}
	if password == "" {
		fatal("empty password (stdin exhausted?)")
	}

	if *useTOTP {
		secret := os.Getenv("MONARCH_TOTP_SECRET")
		if secret == "" {
			secret, err = readSecret("TOTP secret: ")
			if err != nil {
				fatal("%v", err)
			}
		}
		err = client.Auth.LoginWithTOTP(ctx, e, password, secret)
	} else {
		err = client.Auth.Login(ctx, e, password)
		switch {
		case errors.Is(err, monarch.ErrMFARequired):
			code := readLine("Two-factor code: ")
			if code == "" {
				fatal("empty two-factor code (stdin exhausted?)")
			}
			err = client.Auth.LoginWithMFA(ctx, e, password, code)
		case errors.Is(err, monarch.ErrEmailOTPRequired):
			code := readLine("Code sent to your email: ")
			if code == "" {
				fatal("empty email code (stdin exhausted?)")
			}
			err = client.Auth.LoginWithEmailOTP(ctx, e, password, code)
		}
	}
	switch {
	case errors.Is(err, monarch.ErrCaptchaRequired):
		fatal("Cloudflare requires a browser check — log in at app.monarch.com in a browser once, then retry")
	case err != nil:
		fatal("login failed: %v", err)
	}
	// The session store (Keychain on macOS) is written by the library on
	// successful login.
	fmt.Printf("Logged in as %s.\n", e)
	if s := client.Session(); s != nil && s.TokenExpiration != "" {
		fmt.Fprintf(os.Stderr, "warning: server reported a token expiry (%s) — trusted-device may not have been honored; expect to re-login\n", s.TokenExpiration)
	}
}

func cmdLogout(args []string) {
	store, err := sessionStore()
	if err != nil {
		fatal("%v", err)
	}
	if err := store.Delete(); err != nil {
		fatal("%v", err)
	}
	fmt.Println("Logged out.")
}

func cmdWhoami(args []string) {
	client, err := newClient()
	if err != nil {
		fatal("%v", err)
	}
	s := client.Session()
	if s == nil || s.Token == "" {
		fatal("not logged in")
	}
	if s.Email != "" {
		fmt.Printf("Logged in as %s\n", s.Email)
	} else {
		fmt.Println("Authenticated via token")
	}
	if !s.CreatedAt.IsZero() {
		fmt.Printf("Session created: %s\n", s.CreatedAt.Local().Format("2006-01-02 15:04 MST"))
	}
	// A local session can look fine while the token is long dead — ask
	// the server.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if id, err := client.Ping(ctx); err != nil {
		fmt.Printf("Server check: FAILED (%v)\n", err)
		os.Exit(1)
	} else {
		fmt.Printf("Server check: OK (%s)\n", id.Email)
	}
}
