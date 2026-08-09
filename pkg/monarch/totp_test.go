package monarch

import (
	"regexp"
	"testing"
	"time"
)

// rfc6238Secret is base32("12345678901234567890"), the RFC 6238 Appendix B
// SHA-1 test key.
const rfc6238Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPRFC6238Vectors(t *testing.T) {
	// RFC 6238 Appendix B, SHA-1 rows (8 digits).
	vectors := []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, v := range vectors {
		got, err := totpN(rfc6238Secret, time.Unix(v.unix, 0).UTC(), 8)
		if err != nil {
			t.Fatalf("totpN(t=%d): %v", v.unix, err)
		}
		if got != v.want {
			t.Errorf("totpN(t=%d) = %s, want %s", v.unix, got, v.want)
		}
	}
}

func TestGenerateTOTPSixDigits(t *testing.T) {
	got, err := GenerateTOTP(rfc6238Secret, time.Unix(59, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\d{6}$`).MatchString(got) {
		t.Errorf("GenerateTOTP = %q, want 6 digits", got)
	}
	// 6-digit code is the low 6 digits of the 8-digit vector for the same time.
	if got != "287082" {
		t.Errorf("GenerateTOTP(t=59) = %s, want 287082", got)
	}
}

func TestGenerateTOTPSecretNormalization(t *testing.T) {
	base, err := GenerateTOTP(rfc6238Secret, time.Unix(59, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{
		"gezdgnbvgy3tqojqgezdgnbvgy3tqojq",        // lowercase
		"GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ", // spaced
		rfc6238Secret + "======",                  // padded
	} {
		got, err := GenerateTOTP(variant, time.Unix(59, 0).UTC())
		if err != nil {
			t.Fatalf("GenerateTOTP(%q): %v", variant, err)
		}
		if got != base {
			t.Errorf("GenerateTOTP(%q) = %s, want %s", variant, got, base)
		}
	}
}

func TestGenerateTOTPInvalidSecret(t *testing.T) {
	if _, err := GenerateTOTP("not!base32", time.Now()); err == nil {
		t.Error("expected error for invalid base32 secret")
	}
}
