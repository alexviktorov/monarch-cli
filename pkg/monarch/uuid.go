package monarch

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

// newUUIDv4 returns a random RFC 4122 version-4 UUID, used as the device-uuid
// header Monarch requires on login and GraphQL requests.
func newUUIDv4() string {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		// crypto/rand failing is unrecoverable; continuing with a
		// predictable device identity is worse than stopping.
		panic("monarch: crypto/rand unavailable: " + err.Error())
	}
	return formatUUID(b)
}

// deviceUUIDFromToken derives a stable device-uuid for token-only sessions.
// Monarch ties tokens to a device identity, so presenting a fresh random
// UUID on every invocation risks rejection; deriving from the token keeps
// the identity constant across runs, and HMAC keeps the token unrecoverable
// from the header value.
func deviceUUIDFromToken(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("monarch-cli-device"))
	var b [16]byte
	copy(b[:], mac.Sum(nil))
	return formatUUID(b)
}

func formatUUID(b [16]byte) string {
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
