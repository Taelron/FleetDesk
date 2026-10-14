// Package secrets resolves named shared secrets (ADR-F011).
//
// A fleet file names a secret instead of holding its value; FleetDesk
// resolves the name at the point of use. A resolved value is a credential
// under ADR-F007 from that moment on: it is returned as a []byte the caller
// owns and must erase when the operation returns, it is never placed on a
// command line, never written to disk, and never logged. This package is a
// backend: no Bubble Tea or Lipgloss, and it imports only credential.
package secrets

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Provider names. Each fleet has one.
const (
	ProviderAzureKeyVault = "azure-keyvault"
)

// The four ways a resolution fails, one error each, so a caller can tell the
// user which of them it was. None carries any part of the secret.
var (
	// ErrProviderMissing: the provider's binary is not on PATH.
	ErrProviderMissing = errors.New("provider binary not found")
	// ErrNotAuthenticated: the user is not logged in to the provider.
	ErrNotAuthenticated = errors.New("not authenticated")
	// ErrVaultAccess: the vault is unreachable or access is denied.
	ErrVaultAccess = errors.New("vault unreachable or access denied")
	// ErrSecretNotFound: the vault has no secret by that name.
	ErrSecretNotFound = errors.New("secret not found")
	// ErrEmpty: the vault returned an empty value. Never used as one.
	ErrEmpty = errors.New("secret has an empty value")
)

// Config is a fleet's secrets: block.
type Config struct {
	Provider     string
	Vault        string
	Subscription string
}

// Resolver resolves a secret by name. The returned buffer is the caller's:
// it must be erased (credential.Erase) as soon as the operation that needed
// the secret returns. On any error the result is nil and no fallback value is
// ever produced.
type Resolver interface {
	Resolve(name string) ([]byte, error)
}

// Error is a resolution failure. Its message is built from the secret's name
// and the vault only.
type Error struct {
	Kind  error // one of the Err… values
	Name  string
	Vault string
}

func (e *Error) Unwrap() error { return e.Kind }

func (e *Error) Error() string {
	switch e.Kind {
	case ErrProviderMissing:
		return fmt.Sprintf("secret %q: az CLI not found in PATH", e.Name)
	case ErrNotAuthenticated:
		return fmt.Sprintf("secret %q: not logged in to Azure (run 'az login')", e.Name)
	case ErrVaultAccess:
		return fmt.Sprintf("secret %q: vault %q unreachable or access denied", e.Name, e.Vault)
	case ErrSecretNotFound:
		return fmt.Sprintf("secret %q: not found in vault %q", e.Name, e.Vault)
	case ErrEmpty:
		return fmt.Sprintf("secret %q: vault %q returned an empty value", e.Name, e.Vault)
	default:
		return fmt.Sprintf("secret %q: %v", e.Name, e.Kind)
	}
}

// Option configures New.
type Option func(*options)

type options struct{ logger *slog.Logger }

// WithLogger sets the logger. It receives the secret's name, the vault, the
// outcome class and the elapsed time — never a value, never a stderr line.
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }

// New returns the resolver for cfg's provider. Only azure-keyvault exists.
func New(cfg Config, opts ...Option) (Resolver, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.logger == nil {
		o.logger = slog.New(slog.DiscardHandler)
	}
	switch cfg.Provider {
	case ProviderAzureKeyVault:
		return &azKeyVault{vault: cfg.Vault, subscription: cfg.Subscription, logger: o.logger}, nil
	default:
		return nil, fmt.Errorf("secrets provider %q is not supported (supported: %s)", cfg.Provider, ProviderAzureKeyVault)
	}
}

// outcome is the class of a resolution, for the log.
func outcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrProviderMissing):
		return "provider-missing"
	case errors.Is(err, ErrNotAuthenticated):
		return "not-authenticated"
	case errors.Is(err, ErrSecretNotFound):
		return "not-found"
	case errors.Is(err, ErrVaultAccess):
		return "vault-access"
	case errors.Is(err, ErrEmpty):
		return "empty"
	default:
		return "error"
	}
}

// classify maps a failed az invocation's stderr to the error class. stderr is
// read here and nowhere else: it is not logged and not carried by the error.
func classify(stderr string) error {
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "az login"), strings.Contains(s, "aadsts"):
		return ErrNotAuthenticated
	case strings.Contains(s, "secretnotfound"):
		return ErrSecretNotFound
	default:
		return ErrVaultAccess
	}
}
