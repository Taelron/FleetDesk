// Package credential implements the storage and lifecycle primitives
// TAE-21 requires for every credential FleetDesk holds: a fixed-size
// []byte buffer, ownership-transferring construction, and explicit
// erasure.
//
// Ownership: every constructor here takes ownership of the caller's slice
// or hands the caller a fresh buffer the caller then owns — nothing in
// this package copies defensively on the caller's behalf. A caller that
// retains an alias to a slice it has handed over can therefore observe
// erasure directly through that alias, which is what makes erasure
// testable rather than merely asserted.
//
// MaxLen (1024 bytes) is longer than any real passphrase; a fixed cap is
// what lets a buffer be allocated once, at capacity, rather than grown
// per keystroke — growing a slice with append copies the backing array
// and abandons the original unerased.
//
// Four limits are outside this package's reach and are named as such,
// not silently assumed away: GC relocation of a live buffer; swapped
// memory; the packet buffer an SSH channel keeps in its packetPool until
// the session closes; and the string x/crypto/ssh's password auth
// constructs at the moment it dials, referenced by nothing FleetDesk
// holds afterward. Bubble Tea's own 256-byte terminal read buffer,
// beneath the []rune a KeyMsg delivers, is also outside reach — the
// []rune itself is reached and zeroed by the code that consumes it.
//
// A per-operation copy still held by a goroutine that is running when the
// process exits is not erased; nothing erases on a crash or SIGKILL
// either. This is a stated residual (TAE-21 Design Notes D-6), not a
// defect this package claims to close.
package credential

import (
	"errors"
	"io"
	"sync"
)

// MaxLen is the maximum credential length in bytes.
const MaxLen = 1024

// ErrTooLong is returned when a credential exceeds MaxLen.
var ErrTooLong = errors.New("credential exceeds 1024 bytes")

// Erase zeroes b[:cap(b)] — not just b[:len(b)], so a result slice with
// len < cap is still fully scrubbed. Not inlined, so a caller holding an
// alias from before this call can read it back afterward and prove the
// zeroing happened, rather than the compiler treating the buffer as dead
// after this call and folding the read away. Nil-safe.
//
//go:noinline
func Erase(b []byte) {
	b = b[:cap(b)]
	clear(b)
}

// Clone returns a fresh buffer of capacity MaxLen holding a copy of src.
// Refuses a src longer than MaxLen with ErrTooLong.
func Clone(src []byte) ([]byte, error) {
	if len(src) > MaxLen {
		return nil, ErrTooLong
	}
	buf := make([]byte, len(src), MaxLen)
	copy(buf, src)
	return buf, nil
}

// Reader is a one-shot io.Reader and io.WriterTo over a copy of pw
// followed by suffix, which it owns. It erases that copy as soon as the
// last byte has been delivered — via Read or WriteTo — and on an
// explicit Erase call. All access is mutex-guarded, so Erase is
// idempotent and safe to call concurrently with delivery: WriteTo exists
// so a caller like x/crypto/ssh's io.Copy(w, s.Stdin) can hand the slice
// straight to the destination writer instead of routing it through
// io.Copy's own scratch buffer — one less place the bytes are copied to.
type Reader struct {
	mu  sync.Mutex
	buf []byte // nil once erased
	off int
}

// NewReader builds a Reader over a copy of pw with suffix appended.
// Refuses a combined length over MaxLen with ErrTooLong.
func NewReader(pw []byte, suffix string) (*Reader, error) {
	if len(pw)+len(suffix) > MaxLen {
		return nil, ErrTooLong
	}
	buf := make([]byte, len(pw)+len(suffix), MaxLen)
	n := copy(buf, pw)
	copy(buf[n:], suffix)
	return &Reader{buf: buf}, nil
}

// Read implements io.Reader. The underlying buffer is erased as soon as
// its last byte has been copied out, before this call returns — not on
// a later call that observes EOF.
func (r *Reader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buf == nil {
		return 0, io.EOF
	}
	n := copy(p, r.buf[r.off:])
	r.off += n
	if r.off >= len(r.buf) {
		r.eraseLocked()
	}
	return n, nil
}

// WriteTo implements io.WriterTo. Held under the same lock as Read and
// Erase for the whole call, so a concurrent Erase cannot observe or
// produce a torn view of the buffer this writes from.
func (r *Reader) WriteTo(w io.Writer) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buf == nil {
		return 0, nil
	}
	n, err := w.Write(r.buf[r.off:])
	r.off += n
	r.eraseLocked()
	return int64(n), err
}

// Erase zeroes the buffer immediately, whether or not delivery has
// completed. Idempotent.
func (r *Reader) Erase() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eraseLocked()
}

func (r *Reader) eraseLocked() {
	if r.buf == nil {
		return
	}
	Erase(r.buf)
	r.buf = nil
}
