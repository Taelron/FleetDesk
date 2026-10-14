package secrets

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
)

// azTimeout bounds one `az keyvault secret show`.
const azTimeout = 60 * time.Second

// stderrCap bounds how much of az's stderr is kept for classification.
const stderrCap = 4096

type azKeyVault struct {
	vault        string
	subscription string
	logger       *slog.Logger
}

// Resolve reads the secret through the az CLI, using the vault and
// subscription the fleet declares. The value reaches this process through a
// pipe read straight into a fixed-size buffer — never through an intermediate
// copy buffer — and the buffer is erased before Resolve returns; the caller
// gets a fresh credential.Clone.
func (r *azKeyVault) Resolve(name string) (secret []byte, err error) {
	start := time.Now()
	defer func() {
		r.logger.Debug("secret resolve", "name", name, "vault", r.vault, "outcome", outcome(err), "elapsed", time.Since(start))
	}()
	fail := func(kind error) ([]byte, error) { return nil, &Error{Kind: kind, Name: name, Vault: r.vault} }

	az, lookErr := exec.LookPath("az")
	if lookErr != nil {
		return fail(ErrProviderMissing)
	}

	ctx, cancel := context.WithTimeout(context.Background(), azTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, az, "keyvault", "secret", "show", //nolint:gosec // G204: az, argv; vault, subscription and name come from the user's own fleet file
		"--vault-name", r.vault,
		"--name", name,
		"--subscription", r.subscription,
		"--query", "value", "-o", "tsv")

	pr, pw, pipeErr := os.Pipe()
	if pipeErr != nil {
		return fail(ErrVaultAccess)
	}
	defer func() { _ = pr.Close() }()
	var stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = pw, &stderr
	if startErr := cmd.Start(); startErr != nil {
		_ = pw.Close()
		if errors.Is(startErr, exec.ErrNotFound) {
			return fail(ErrProviderMissing)
		}
		return fail(ErrVaultAccess)
	}
	_ = pw.Close() // the child holds the write end; reading ends when it exits

	// MaxLen of value, one byte for the newline az prints, one byte of slack
	// that tells "fits" from "does not".
	var buf [credential.MaxLen + 2]byte
	defer credential.Erase(buf[:])
	n, tooLong := readFixed(pr, buf[:])
	if tooLong {
		_ = cmd.Process.Kill() // do not let an oversized value keep streaming
	}
	waitErr := cmd.Wait()
	if tooLong {
		return nil, &Error{Kind: credential.ErrTooLong, Name: name, Vault: r.vault}
	}
	if waitErr != nil {
		return fail(classify(stderr.String()))
	}

	val := buf[:n]
	if len(val) > 0 && val[len(val)-1] == '\n' {
		val = val[:len(val)-1]
	}
	if len(val) == 0 {
		return fail(ErrEmpty)
	}
	secret, cloneErr := credential.Clone(val)
	if cloneErr != nil {
		return nil, &Error{Kind: cloneErr, Name: name, Vault: r.vault}
	}
	return secret, nil
}

// readFixed fills buf from r until EOF, with no intermediate buffer. tooLong
// means the value filled buf: buf is sized so that the longest legal
// credential plus the newline az prints always leaves a byte unused.
func readFixed(r io.Reader, buf []byte) (n int, tooLong bool) {
	for {
		if n == len(buf) {
			return n, true
		}
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, false
		}
	}
}

// limitedBuffer keeps the first stderrCap bytes written to it and discards
// the rest.
type limitedBuffer struct{ b []byte }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := stderrCap - len(l.b); room > 0 {
		l.b = append(l.b, p[:min(len(p), room)]...)
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string { return string(l.b) }
