//go:build keyvault

package secrets

import (
	"fmt"
	"os"
	"testing"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
)

// TestL1KeyVaultResolve is TAE-116's L1 — the one human step: resolve one
// real secret from a real Key Vault with the real az, and report that it was
// read and erased. It prints the secret's length and never its value.
//
//	make keyvault-resolve VAULT=<vault> SUBSCRIPTION=<id> NAME=<secret>
func TestL1KeyVaultResolve(t *testing.T) {
	vault, sub, name := os.Getenv("FLEETDESK_KV_VAULT"), os.Getenv("FLEETDESK_KV_SUBSCRIPTION"), os.Getenv("FLEETDESK_KV_NAME")
	if vault == "" || sub == "" || name == "" {
		t.Fatal("FLEETDESK_KV_VAULT, FLEETDESK_KV_SUBSCRIPTION and FLEETDESK_KV_NAME must be set (use `make keyvault-resolve`)")
	}
	r, err := New(Config{Provider: ProviderAzureKeyVault, Vault: vault, Subscription: sub})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := r.Resolve(name)
	if err != nil {
		t.Fatalf("%v", err) // the message names the secret and the vault, never the value
	}
	n := len(secret)
	alias := secret[:cap(secret)]
	credential.Erase(secret)
	for i, b := range alias {
		if b != 0 {
			t.Fatalf("byte %d of the resolved buffer survived erasure", i)
		}
	}
	fmt.Printf("resolved %s from %s: %d bytes, erased\n", name, vault, n)
}
