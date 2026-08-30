package credential

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

// readByte is deliberately non-inlinable, via an index, so a test holding
// an alias to a buffer cannot have its post-erase check folded away by the
// compiler treating the buffer as dead after the call that erased it.
//
//go:noinline
func readByte(b []byte, i int) byte { return b[i] }

func allZero(t *testing.T, b []byte) bool {
	t.Helper()
	for i := range b {
		if readByte(b, i) != 0 {
			return false
		}
	}
	return true
}

func TestEraseZeroesFullCapacityThroughAlias(t *testing.T) {
	full := make([]byte, 8, 16) // len 8, cap 16 — a result slice shape
	copy(full, "password")
	alias := full // retained before Erase, per the issue's own methodology

	Erase(full)

	if !allZero(t, alias[:cap(alias)]) {
		t.Errorf("expected Erase to zero the full capacity, not just len; alias[:cap] = %q", alias[:cap(alias)])
	}
}

func TestEraseNilSafe(t *testing.T) {
	Erase(nil)
}

func TestCloneIndependentCopy(t *testing.T) {
	src := []byte("hunter2")
	clone, err := Clone(src)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if string(clone) != "hunter2" {
		t.Fatalf("Clone content = %q, want %q", clone, "hunter2")
	}
	if cap(clone) != MaxLen {
		t.Errorf("Clone cap = %d, want %d", cap(clone), MaxLen)
	}
	Erase(src)
	if string(clone) != "hunter2" {
		t.Errorf("Clone was affected by erasing src — not independent: %q", clone)
	}
}

func TestCloneRefusesOverMax(t *testing.T) {
	src := make([]byte, MaxLen+1)
	if _, err := Clone(src); err != ErrTooLong {
		t.Errorf("Clone(%d bytes) err = %v, want ErrTooLong", len(src), err)
	}
}

func TestReaderDeliversExactlyPasswordPlusSuffixThenEOF(t *testing.T) {
	r, err := NewReader([]byte("hunter2"), "\n")
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hunter2\n" {
		t.Errorf("delivered %q, want %q", data, "hunter2\n")
	}
	n, err := r.Read(make([]byte, 1))
	if n != 0 || err != io.EOF {
		t.Errorf("Read after delivery: n=%d err=%v, want n=0 err=io.EOF", n, err)
	}
}

func TestReaderBufferZeroAfterReadDelivery(t *testing.T) {
	r, err := NewReader([]byte("hunter2"), "\n")
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	buf := r.buf // alias held before delivery
	if _, err := io.ReadAll(r); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !allZero(t, buf[:cap(buf)]) {
		t.Errorf("expected the Reader's buffer to be zero after full delivery via Read; alias = %q", buf[:cap(buf)])
	}
}

func TestReaderBufferZeroAfterWriteToDelivery(t *testing.T) {
	r, err := NewReader([]byte("hunter2"), "\n")
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	buf := r.buf
	var out bytes.Buffer
	n, err := r.WriteTo(&out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if out.String() != "hunter2\n" || n != int64(len("hunter2\n")) {
		t.Errorf("WriteTo delivered (%q, %d), want (%q, %d)", out.String(), n, "hunter2\n", len("hunter2\n"))
	}
	if !allZero(t, buf[:cap(buf)]) {
		t.Errorf("expected the Reader's buffer to be zero after full delivery via WriteTo; alias = %q", buf[:cap(buf)])
	}
}

func TestReaderEraseBeforeDeliveryYieldsNoData(t *testing.T) {
	r, err := NewReader([]byte("hunter2"), "\n")
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	buf := r.buf
	r.Erase()
	if !allZero(t, buf[:cap(buf)]) {
		t.Errorf("expected Erase before delivery to zero the buffer; alias = %q", buf[:cap(buf)])
	}
	n, err := r.Read(make([]byte, 8))
	if n != 0 || err != io.EOF {
		t.Errorf("Read after Erase-before-delivery: n=%d err=%v, want n=0 err=io.EOF", n, err)
	}
}

func TestReaderRefusesOverMax(t *testing.T) {
	pw := make([]byte, MaxLen)
	if _, err := NewReader(pw, "\n"); err != ErrTooLong {
		t.Errorf("NewReader with pw+suffix over MaxLen: err = %v, want ErrTooLong", err)
	}
}

func TestReaderConcurrentEraseAndWriteTo(t *testing.T) {
	// Run under -race: WriteTo and Erase touch the same buffer from two
	// goroutines; the mutex must make every access well-ordered.
	for range 200 {
		r, err := NewReader([]byte("hunter2"), "\n")
		if err != nil {
			t.Fatalf("NewReader: %v", err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			var out strings.Builder
			_, _ = r.WriteTo(&out)
		}()
		go func() {
			defer wg.Done()
			r.Erase()
		}()
		wg.Wait()
	}
}
