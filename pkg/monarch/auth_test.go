package monarch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

type memStore struct {
	s     *Session
	saves int
}

func (m *memStore) Load() (*Session, error) {
	if m.s == nil {
		return nil, ErrNoSession
	}
	return m.s, nil
}
func (m *memStore) Save(s *Session) error { m.s = s; m.saves++; return nil }
func (m *memStore) Delete() error         { m.s = nil; return nil }

func decodeLogin(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode login body: %v", err)
	}
	return body
}

func TestLoginSuccess(t *testing.T) {
	store := &memStore{}
	var gotBody map[string]any
	var gotDevice, gotPlatform string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login/" {
			t.Errorf("path = %s, want /auth/login/", r.URL.Path)
		}
		gotBody = decodeLogin(t, r)
		gotDevice = r.Header.Get("device-uuid")
		gotPlatform = r.Header.Get("Client-Platform")
		w.Write([]byte(`{"token":"tok-abc","userId":"user-1"}`))
	}), WithSessionStore(store))

	if err := c.Auth.Login(context.Background(), "a@b.c", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if gotPlatform != "web" || gotDevice == "" {
		t.Errorf("headers: Client-Platform=%q device-uuid=%q", gotPlatform, gotDevice)
	}
	for _, key := range []string{"username", "password", "trusted_device", "supports_mfa", "supports_email_otp", "supports_recaptcha"} {
		if _, ok := gotBody[key]; !ok {
			t.Errorf("login body missing %q", key)
		}
	}
	if gotBody["username"] != "a@b.c" {
		t.Errorf("username = %v", gotBody["username"])
	}
	s := c.Session()
	if s == nil || s.Token != "tok-abc" || s.UserID != "user-1" || s.Email != "a@b.c" {
		t.Fatalf("session = %+v", s)
	}
	if s.DeviceUUID != gotDevice {
		t.Errorf("session DeviceUUID %q != sent header %q", s.DeviceUUID, gotDevice)
	}
	if s.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
	if store.saves != 1 || store.s.Token != "tok-abc" {
		t.Errorf("store: saves=%d s=%+v", store.saves, store.s)
	}
}

func TestLoginMFAFlow(t *testing.T) {
	var devices []string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeLogin(t, r)
		devices = append(devices, r.Header.Get("device-uuid"))
		if body["totp"] == "123456" {
			w.Write([]byte(`{"token":"tok-mfa","userId":"u"}`))
			return
		}
		// Monarch signals MFA via error_code in the body; the HTTP status
		// accompanying it is not part of the contract.
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error_code":"MFA_REQUIRED","message":"MFA required"}`))
	}))

	err := c.Auth.Login(context.Background(), "a@b.c", "pw")
	if !errors.Is(err, ErrMFARequired) {
		t.Fatalf("err = %v, want ErrMFARequired", err)
	}
	if err := c.Auth.LoginWithMFA(context.Background(), "a@b.c", "pw", "123456"); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0] != devices[1] {
		t.Errorf("device-uuid must stay stable across the MFA retry: %v", devices)
	}
	if c.Session().Token != "tok-mfa" {
		t.Errorf("session = %+v", c.Session())
	}
}

func TestLoginEmailOTPFlow(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeLogin(t, r)
		if body["email_otp"] == "999000" {
			w.Write([]byte(`{"token":"tok-otp"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error_code":"EMAIL_OTP_REQUIRED"}`))
	}))
	err := c.Auth.Login(context.Background(), "a@b.c", "pw")
	if !errors.Is(err, ErrEmailOTPRequired) {
		t.Fatalf("err = %v, want ErrEmailOTPRequired", err)
	}
	if err := c.Auth.LoginWithEmailOTP(context.Background(), "a@b.c", "pw", "999000"); err != nil {
		t.Fatal(err)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error_code":"INVALID_CREDENTIALS"}`))
	}))
	if err := c.Auth.Login(context.Background(), "a@b.c", "wrong"); !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("err = %v, want ErrInvalidLogin", err)
	}
}

func TestLoginWithTOTPGeneratesCode(t *testing.T) {
	var gotTOTP string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeLogin(t, r)
		gotTOTP, _ = body["totp"].(string)
		w.Write([]byte(`{"token":"tok"}`))
	}))
	if err := c.Auth.LoginWithTOTP(context.Background(), "a@b.c", "pw", rfc6238Secret); err != nil {
		t.Fatal(err)
	}
	if len(gotTOTP) != 6 {
		t.Errorf("totp = %q, want a 6-digit code", gotTOTP)
	}
}

func TestLoginMissingToken(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	if err := c.Auth.Login(context.Background(), "a@b.c", "pw"); err == nil {
		t.Fatal("expected error for token-less 200")
	}
}

func TestLoginCaptchaRequired(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error_code":"CAPTCHA_REQUIRED"}`))
	}))
	if err := c.Auth.Login(context.Background(), "a@b.c", "pw"); !errors.Is(err, ErrCaptchaRequired) {
		t.Fatalf("err = %v, want ErrCaptchaRequired (NOT invalid-credentials)", err)
	}
}

func TestLoginDetailFallbackClassification(t *testing.T) {
	cases := []struct {
		detail string
		want   error
	}{
		{"Captcha verification required", ErrCaptchaRequired},
		{"Multi-Factor authentication code required", ErrMFARequired},
		{"A one-time code was sent to your email", ErrEmailOTPRequired},
	}
	for _, tc := range cases {
		c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"detail":"` + tc.detail + `"}`))
		}))
		if err := c.Auth.Login(context.Background(), "a@b.c", "pw"); !errors.Is(err, tc.want) {
			t.Errorf("detail %q: err = %v, want %v", tc.detail, err, tc.want)
		}
	}
}

func TestLoginRejectsJWTShapedToken(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"token":"eyJhbGciOi.eyJzdWIiOi.c2lnbmF0dXJl","userId":"u"}`))
	}))
	err := c.Auth.Login(context.Background(), "a@b.c", "pw")
	if err == nil {
		t.Fatal("a JWT-shaped (short-lived features) token must be rejected")
	}
	if c.Session() != nil {
		t.Error("rejected token must not be persisted as a session")
	}
}

func TestLoginRecordsTokenExpiration(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"token":"tok","userId":"u","tokenExpiration":"2026-08-09T12:00:00Z"}`))
	}))
	if err := c.Auth.Login(context.Background(), "a@b.c", "pw"); err != nil {
		t.Fatal(err)
	}
	if got := c.Session().TokenExpiration; got != "2026-08-09T12:00:00Z" {
		t.Errorf("TokenExpiration = %q, want recorded", got)
	}
}

func TestClientPing(t *testing.T) {
	c := gqlServer(t, "GetIdentity", `{"me":{"id":"u1","email":"a@b.c"}}`, nil)
	id, err := c.Ping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id.ID != "u1" || id.Email != "a@b.c" {
		t.Errorf("identity = %+v", id)
	}
	bad := gqlServer(t, "GetIdentity", `{"me":null}`, nil)
	if _, err := bad.Ping(context.Background()); err == nil {
		t.Error("null me must be an error")
	}
}

func TestLoginNeverRetries(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	if err := c.Auth.Login(context.Background(), "a@b.c", "pw"); err == nil {
		t.Fatal("expected error")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1 (credentials must not be replayed)", n)
	}
}
