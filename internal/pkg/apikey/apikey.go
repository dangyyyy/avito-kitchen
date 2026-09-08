package apikey

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns the value stored in restaurants.api_key_hash. Only the hash is
// persisted, so a database dump does not hand over working partner keys.
func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))

	return hex.EncodeToString(sum[:])
}
