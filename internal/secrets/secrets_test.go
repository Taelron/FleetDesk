package secrets

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
)

const stubValue = "stub-secret-value"

// stubPath puts the repository's stand-in az first on PATH.
func stubPath(t *testing.T) {
	t.Helper()
	dir, err := filepath.Abs("../../test/azstub")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
}

func resolver(t *testing.T, vault string, opts ...Option) Resolver {
	t.Helper()
	r, err := New(Config{Provider: ProviderAzureKeyVault, Vault: vault, Subscription: "sub-1"}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResolve_Success(t *testing.T) {
	stubPath(t)
	got, err := resolver(t, "stub-vault").Resolve("stub-ok")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != stubValue {
		t.Errorf("value = %q, want %q (one trailing newline trimmed, no more)", got, stubValue)
	}
	if cap(got) != credential.MaxLen {
		t.Errorf("cap = %d: the result must be a credential.Clone (fixed %d)", cap(got), credential.MaxLen)
	}
	credential.Erase(got)
}

func TestResolve_FourDistinctFailures(t *testing.T) {
	stubPath(t)
	cases := []struct {
		name, vault, secret string
		kind                error
		msg                 string
		noAz                bool
	}{
		{"az missing", "stub-vault", "stub-ok", ErrProviderMissing, `secret "stub-ok": az CLI not found in PATH`, true},
		{"not logged in", "stub-nologin", "stub-ok", ErrNotAuthenticated, `secret "stub-ok": not logged in to Azure (run 'az login')`, false},
		{"forbidden", "stub-vault", "stub-forbidden", ErrVaultAccess, `secret "stub-forbidden": vault "stub-vault" unreachable or access denied`, false},
		{"not found", "stub-vault", "stub-missing", ErrSecretNotFound, `secret "stub-missing": not found in vault "stub-vault"`, false},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.noAz {
				t.Setenv("PATH", t.TempDir())
			}
			got, err := resolver(t, c.vault).Resolve(c.secret)
			if got != nil {
				t.Errorf("a failure never yields a value, got %q", got)
			}
			if !errors.Is(err, c.kind) {
				t.Fatalf("err = %v, want kind %v", err, c.kind)
			}
			if err.Error() != c.msg {
				t.Errorf("message = %q, want %q", err.Error(), c.msg)
			}
			if strings.Contains(err.Error(), stubValue) {
				t.Errorf("a failure message must hold no part of the secret: %q", err)
			}
			seen[err.Error()] = true
		})
	}
	if len(seen) != 4 {
		t.Errorf("want four distinct messages, got %d", len(seen))
	}
}

// az is replaced by a script that prints what the test tells it to.
func fakeAz(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "az"), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
}

func TestResolve_OversizedValueIsRefused(t *testing.T) {
	fakeAz(t, "head -c 5000 /dev/zero | tr '\\0' x\n")
	got, err := resolver(t, "v").Resolve("big")
	if got != nil || !errors.Is(err, credential.ErrTooLong) {
		t.Fatalf("got %q, %v; want nil and ErrTooLong", got, err)
	}
	// exactly at the limit is fine, with the newline az prints
	fakeAz(t, "head -c 1024 /dev/zero | tr '\\0' y; echo\n")
	got, err = resolver(t, "v").Resolve("max")
	if err != nil || len(got) != credential.MaxLen {
		t.Fatalf("a %d-byte value must resolve: %d bytes, %v", credential.MaxLen, len(got), err)
	}
	credential.Erase(got)
}

func TestResolve_EmptyValueIsNeverReturned(t *testing.T) {
	fakeAz(t, "echo\n")
	got, err := resolver(t, "v").Resolve("blank")
	if got != nil || !errors.Is(err, ErrEmpty) {
		t.Fatalf("got %q, %v; want nil and ErrEmpty", got, err)
	}
}

func TestResolve_UnknownFailureIsAccessNotSuccess(t *testing.T) {
	fakeAz(t, "echo 'something odd' >&2; exit 3\n")
	got, err := resolver(t, "v").Resolve("n")
	if got != nil || !errors.Is(err, ErrVaultAccess) {
		t.Fatalf("got %q, %v; want nil and ErrVaultAccess", got, err)
	}
	// a non-zero exit with a value on stdout is still a failure
	fakeAz(t, "echo leaked-value; echo '(Forbidden)' >&2; exit 1\n")
	got, err = resolver(t, "v").Resolve("n")
	if got != nil || err == nil || strings.Contains(err.Error(), "leaked-value") {
		t.Fatalf("got %q, %v", got, err)
	}
}

// The secret name and vault are the only things on az's argv; the value is
// never an argument (it is an output), and az is given no stdin.
func TestResolve_ArgvCarriesNoValue(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	script := "printf '%s\\n' \"$@\" > " + argvFile + "\necho " + stubValue + "\n"
	fakeAz(t, script)
	got, err := resolver(t, "my-vault").Resolve("my-name")
	if err != nil {
		t.Fatal(err)
	}
	credential.Erase(got)
	argv, _ := os.ReadFile(argvFile)
	for _, want := range []string{"keyvault", "secret", "show", "--vault-name", "my-vault", "--name", "my-name", "--subscription", "sub-1", "--query", "value", "tsv"} {
		if !bytes.Contains(argv, []byte(want)) {
			t.Errorf("argv lacks %q:\n%s", want, argv)
		}
	}
	if bytes.Contains(argv, []byte(stubValue)) {
		t.Errorf("the value is on az's argv:\n%s", argv)
	}
}

// The log records name, vault, outcome and elapsed time — never the value or
// a stderr line.
func TestResolve_LogsOutcomeNeverValue(t *testing.T) {
	stubPath(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := resolver(t, "stub-vault", WithLogger(log))
	ok, _ := r.Resolve("stub-ok")
	credential.Erase(ok)
	_, _ = r.Resolve("stub-forbidden")
	_, _ = r.Resolve("stub-missing")
	out := buf.String()
	for _, want := range []string{"name=stub-ok", "outcome=ok", "outcome=vault-access", "outcome=not-found", "vault=stub-vault", "elapsed="} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{stubValue, "Forbidden", "SecretNotFound", "permission"} {
		if strings.Contains(out, bad) {
			t.Errorf("log must not carry %q:\n%s", bad, out)
		}
	}
}

func TestNew_OnlyAzureKeyVault(t *testing.T) {
	if _, err := New(Config{Provider: "hashicorp-vault"}); err == nil || !strings.Contains(err.Error(), "supported: azure-keyvault") {
		t.Errorf("err = %v", err)
	}
	if _, err := New(Config{}); err == nil {
		t.Error("an empty provider is not a provider")
	}
}

func TestReadFixed(t *testing.T) {
	buf := make([]byte, 8)
	if n, tooLong := readFixed(strings.NewReader("1234"), buf); n != 4 || tooLong {
		t.Errorf("short: %d %v", n, tooLong)
	}
	if n, tooLong := readFixed(strings.NewReader("12345678"), buf); n != 8 || !tooLong {
		t.Errorf("exactly full counts as overflow (one byte of slack is reserved): %d %v", n, tooLong)
	}
	if _, tooLong := readFixed(io.LimitReader(strings.NewReader(strings.Repeat("x", 100)), 100), buf); !tooLong {
		t.Error("overflow")
	}
}

func TestLimitedBuffer(t *testing.T) {
	var l limitedBuffer
	n, err := l.Write(bytes.Repeat([]byte("e"), stderrCap+500))
	if n != stderrCap+500 || err != nil {
		t.Fatalf("Write must report the full length so the child is never blocked: %d %v", n, err)
	}
	if len(l.String()) != stderrCap {
		t.Errorf("kept %d bytes, want %d", len(l.String()), stderrCap)
	}
	n, _ = l.Write([]byte("more"))
	if n != 4 || len(l.String()) != stderrCap {
		t.Errorf("a full buffer discards: %d %d", n, len(l.String()))
	}
}

func TestErrorUnwrap(t *testing.T) {
	var e error = &Error{Kind: ErrSecretNotFound, Name: "n", Vault: "v"}
	if !errors.Is(e, ErrSecretNotFound) || errors.Is(e, ErrVaultAccess) {
		t.Error("errors.Is must see the kind")
	}
}
