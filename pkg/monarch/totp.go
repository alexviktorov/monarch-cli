package monarch

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// GenerateTOTP returns the 6-digit RFC 6238 code for a base32 secret at time
// t, using a 30-second step and HMAC-SHA1 — the parameters Monarch uses.
// Secrets are accepted in upper or lower case, with or without spaces and
// base32 padding.
func GenerateTOTP(secret string, t time.Time) (string, error) {
	return totpN(secret, t, 6)
}

// totpN exists (with a digits parameter) so the RFC 6238 Appendix B test
// vectors, which are 8-digit, exercise the exact production code path.
func totpN(secret string, t time.Time, digits int) (string, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	normalized = strings.TrimRight(normalized, "=")
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return "", fmt.Errorf("monarch: invalid TOTP secret: %w", err)
	}

	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(t.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)

	// RFC 4226 §5.3 dynamic truncation.
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, code%mod), nil
}
