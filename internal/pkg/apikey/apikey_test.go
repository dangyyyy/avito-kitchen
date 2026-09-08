package apikey_test

import (
	"testing"

	"github.com/dangyyyy/avito-kitchen/internal/pkg/apikey"
)

func TestHash(t *testing.T) {
	t.Parallel()

	// The demo key shipped in seed data; keep this in sync with the seed migration.
	const (
		demoKey  = "demo-restaurant-key"
		demoHash = "4ed712d28db8409911fff6151f226c3ca6309e770b3df44f63e62100d4e29543"
	)

	if got := apikey.Hash(demoKey); got != demoHash {
		t.Fatalf("expected %s, got %s", demoHash, got)
	}

	if apikey.Hash("another-key") == demoHash {
		t.Fatal("different keys must not collide")
	}
}
