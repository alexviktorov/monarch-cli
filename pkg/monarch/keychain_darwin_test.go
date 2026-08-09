//go:build darwin

package monarch

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeSecurity struct {
	calls []struct {
		stdin string
		args  []string
	}
	stdout string
	exit   int
}

func (f *fakeSecurity) run(stdin string, args ...string) (string, int, error) {
	f.calls = append(f.calls, struct {
		stdin string
		args  []string
	}{stdin, args})
	return f.stdout, f.exit, nil
}

func TestKeychainSaveKeepsTokenOutOfArgv(t *testing.T) {
	fake := &fakeSecurity{}
	store := &keychainStore{run: fake.run}
	s := &Session{Token: "super-secret-token", Email: "a@b.c", DeviceUUID: "dev"}
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls = %d", len(fake.calls))
	}
	call := fake.calls[0]
	if len(call.args) != 1 || call.args[0] != "-i" {
		t.Errorf("args = %v, want [-i] only — anything else risks argv exposure", call.args)
	}
	for _, a := range call.args {
		if strings.Contains(a, s.Token) || strings.Contains(a, base64.StdEncoding.EncodeToString([]byte(s.Token))) {
			t.Fatalf("token leaked into argv: %v", call.args)
		}
	}
	prefix := fmt.Sprintf("add-generic-password -U -s %s -a %s -w ", keychainService, keychainAccount)
	if !strings.HasPrefix(call.stdin, prefix) || !strings.HasSuffix(call.stdin, "\n") {
		t.Fatalf("stdin = %q, want %q<payload>\\n", call.stdin, prefix)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(call.stdin, prefix), "\n")
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	var got Session
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Token != s.Token || got.Email != s.Email {
		t.Errorf("round-tripped session = %+v", got)
	}
}

func TestKeychainLoad(t *testing.T) {
	data, _ := json.Marshal(&Session{Token: "tok", DeviceUUID: "dev"})
	fake := &fakeSecurity{stdout: base64.StdEncoding.EncodeToString(data) + "\n"}
	store := &keychainStore{run: fake.run}
	s, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Token != "tok" || s.DeviceUUID != "dev" {
		t.Errorf("session = %+v", s)
	}
	call := fake.calls[0]
	want := []string{"find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w"}
	if len(call.args) != len(want) {
		t.Fatalf("args = %v, want %v", call.args, want)
	}
	for i := range want {
		if call.args[i] != want[i] {
			t.Fatalf("args = %v, want %v", call.args, want)
		}
	}
}

func TestKeychainLoadNotFound(t *testing.T) {
	fake := &fakeSecurity{exit: exitItemNotFound}
	store := &keychainStore{run: fake.run}
	if _, err := store.Load(); !errors.Is(err, ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
}

func TestKeychainSaveFailure(t *testing.T) {
	fake := &fakeSecurity{exit: 1}
	store := &keychainStore{run: fake.run}
	if err := store.Save(&Session{Token: "t"}); err == nil {
		t.Error("expected error on non-zero exit")
	}
}

func TestKeychainProbe(t *testing.T) {
	for _, tc := range []struct {
		exit int
		ok   bool
	}{
		{0, true}, {exitItemNotFound, true}, {36, false}, {1, false},
	} {
		fake := &fakeSecurity{exit: tc.exit}
		store := &keychainStore{run: fake.run}
		err := store.probe()
		if (err == nil) != tc.ok {
			t.Errorf("probe with exit %d: err = %v, want ok=%v", tc.exit, err, tc.ok)
		}
	}
}

func TestKeychainDeleteIdempotent(t *testing.T) {
	fake := &fakeSecurity{exit: exitItemNotFound}
	store := &keychainStore{run: fake.run}
	if err := store.Delete(); err != nil {
		t.Errorf("Delete of a missing item = %v, want nil", err)
	}
}
