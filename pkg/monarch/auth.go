package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"time"
)

type AuthService struct {
	c *Client
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
		Token     string `json:"token"`
		UserID    string `json:"userId"`
		ErrorCode string `json:"error_code"`
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
	case "INVALID_CREDENTIALS":
		return ErrInvalidLogin
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

	a.c.session = &Session{
		Token:      resp.Token,
		Email:      email,
		UserID:     resp.UserID,
		DeviceUUID: a.deviceUUID,
		CreatedAt:  time.Now().UTC(),
	}
	if a.c.store != nil {
		if err := a.c.store.Save(a.c.session); err != nil {
			return fmt.Errorf("monarch: logged in but failed to save session: %w", err)
		}
	}
	return nil
}
