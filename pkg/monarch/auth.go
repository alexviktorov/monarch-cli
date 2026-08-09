package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	detailCaptchaRe  = regexp.MustCompile(`(?i)captcha`)
	detailMFARe      = regexp.MustCompile(`(?i)multi.?factor|two.?factor|\bmfa\b`)
	detailEmailOTPRe = regexp.MustCompile(`(?i)email.{0,40}(code|otp)|(code|otp).{0,40}email`)
)

// classifyLoginDetail maps a prose challenge description to the matching
// sentinel, or nil when it doesn't describe a known challenge.
func classifyLoginDetail(detail string) error {
	switch {
	case detail == "":
		return nil
	case detailCaptchaRe.MatchString(detail):
		return ErrCaptchaRequired
	case detailMFARe.MatchString(detail):
		return ErrMFARequired
	case detailEmailOTPRe.MatchString(detail):
		return ErrEmailOTPRequired
	}
	return nil
}

type AuthService struct {
	c  *Client
	mu sync.Mutex // serializes logins (deviceUUID init + session swap)
	// deviceUUID is generated once per AuthService and reused across the
	// MFA retry — Monarch ties the challenge to the device identity, so
	// the second POST must present the same UUID as the first.
	deviceUUID string
}

// Login authenticates with email + password. When the account has 2FA it
// returns ErrMFARequired or ErrEmailOTPRequired; retry with LoginWithMFA or
// LoginWithEmailOTP.
func (a *AuthService) Login(ctx context.Context, email, password string) error {
	return a.login(ctx, email, password, nil)
}

// LoginWithMFA retries login with a 6-digit authenticator code.
func (a *AuthService) LoginWithMFA(ctx context.Context, email, password, code string) error {
	return a.login(ctx, email, password, map[string]any{"totp": code})
}

// LoginWithEmailOTP retries login with the code Monarch emailed.
func (a *AuthService) LoginWithEmailOTP(ctx context.Context, email, password, code string) error {
	return a.login(ctx, email, password, map[string]any{"email_otp": code})
}

// LoginWithTOTP generates the current code from a TOTP secret and logs in.
func (a *AuthService) LoginWithTOTP(ctx context.Context, email, password, totpSecret string) error {
	code, err := GenerateTOTP(totpSecret, time.Now())
	if err != nil {
		return err
	}
	return a.LoginWithMFA(ctx, email, password, code)
}

func (a *AuthService) login(ctx context.Context, email, password string, extra map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deviceUUID == "" {
		a.deviceUUID = newUUIDv4()
	}
	body := map[string]any{
		"username":           email,
		"password":           password,
		"trusted_device":     true,
		"supports_mfa":       true,
		"supports_email_otp": true,
		"supports_recaptcha": true,
	}
	maps.Copy(body, extra)
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("monarch: Login: encode request: %w", err)
	}
	build := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.c.baseURL+loginPath, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		a.c.setCommonHeaders(req.Header, a.deviceUUID)
		return req, nil
	}
	// Logins are never retried: an ambiguous network failure must not
	// replay credentials, and MFA codes are single-use anyway.
	status, respBody, err := a.c.do(ctx, "Login", build, false)
	if err != nil {
		return err
	}

	var resp struct {
		Token           string          `json:"token"`
		UserID          string          `json:"userId"`
		ErrorCode       string          `json:"error_code"`
		Detail          string          `json:"detail"`
		TokenExpiration json.RawMessage `json:"tokenExpiration"`
	}
	// The body may be empty or non-JSON on some statuses; error_code
	// simply stays empty then.
	_ = json.Unmarshal(respBody, &resp)

	// error_code is authoritative and is checked BEFORE the HTTP status:
	// Monarch signals the MFA challenge in the body, and the status code
	// accompanying it is not part of the contract.
	switch resp.ErrorCode {
	case "MFA_REQUIRED":
		return ErrMFARequired
	case "EMAIL_OTP_REQUIRED":
		return ErrEmailOTPRequired
	case "CAPTCHA_REQUIRED":
		return ErrCaptchaRequired
	case "INVALID_CREDENTIALS":
		return ErrInvalidLogin
	}
	// Fallback: some deployments signal the challenge only in a prose
	// `detail` field.
	if err := classifyLoginDetail(resp.Detail); err != nil {
		return err
	}
	if status < 200 || status > 299 {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return ErrInvalidLogin
		}
		return apiError("Login", status, respBody)
	}
	if resp.Token == "" {
		return fmt.Errorf("monarch: login response contained no token")
	}
	// A JWT-shaped token (two dots) is the short-lived "features" token,
	// not the session token — persisting it means dying in ~an hour.
	if strings.Count(resp.Token, ".") == 2 {
		return errors.New("monarch: login returned a short-lived JWT instead of a session token (trusted-device/MFA likely not honored) — retry with MFA")
	}

	a.c.session = &Session{
		Token:      resp.Token,
		Email:      email,
		UserID:     resp.UserID,
		DeviceUUID: a.deviceUUID,
		CreatedAt:  time.Now().UTC(),
	}
	// Normally null for trusted-device logins; recorded when the server
	// reports a real expiry so whoami/login can warn about it.
	if exp := strings.Trim(string(resp.TokenExpiration), `"`); exp != "" && exp != "null" {
		a.c.session.TokenExpiration = exp
	}
	if a.c.store != nil {
		if err := a.c.store.Save(a.c.session); err != nil {
			return fmt.Errorf("monarch: logged in but failed to save session: %w", err)
		}
	}
	return nil
}
