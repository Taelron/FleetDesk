package ssh

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
)

func managerWithSudo(t *testing.T, idx int, pw string) *Manager {
	t.Helper()
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	sm.SetSudoPassword(idx, mustClone(t, pw))
	return sm
}

func TestRewriteSudoWithSecret_Shape(t *testing.T) {
	const pw, secret = "hunter2-pw", "ak-very-secret"
	sm := managerWithSudo(t, 0, pw)
	cmd, r, err := sm.RewriteSudoWithSecret(0, "sudo subscription-manager clean && sudo dnf install -y x", []byte(secret), "FLEETDESK_AK", "python3 -c 'print(1)' 'org'")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Erase()

	for _, bad := range []string{pw, secret} {
		if strings.Contains(cmd, bad) {
			t.Errorf("the command string must hold neither the password nor the secret; found %q in:\n%s", bad, cmd)
		}
	}
	if strings.Count(cmd, sudoPreamble) != 1 || !strings.HasPrefix(cmd, sudoPreamble) {
		t.Errorf("exactly one preamble, first:\n%s", cmd)
	}
	if !strings.Contains(cmd, "IFS= read -r FLEETDESK_SECRET || exit 95; ") {
		t.Errorf("the secret is read from its own stdin line:\n%s", cmd)
	}
	// both prefix occurrences go through the relay; no bare "sudo " remains
	if got := strings.Count(cmd, `| sudo -S 2>/dev/null `); got != 3 {
		t.Errorf("want 3 password relays (2 in the prefix, 1 for the consumer), got %d:\n%s", got, cmd)
	}
	for _, want := range []string{
		`( printf '%s\n' "FLEETDESK_AK=$FLEETDESK_SECRET" > "$d/s" ) & w=$!; printf '%s\n' "$FLEETDESK_SUDO_PW" | sudo -S 2>/dev/null sh -c 'exec <"$0"; python3 -c '\''print(1)'\'' '\''org'\''' "$d/s"; rc=$?; kill $w 2>/dev/null; rm -rf "$d"; exit $rc`,
		`d=$(mktemp -d) || exit 93; mkfifo "$d/s"`,
		"subscription-manager clean && ",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q in:\n%s", want, cmd)
		}
	}
	if strings.Contains(strings.ReplaceAll(cmd, `| sudo -S`, ""), "sudo ") {
		t.Errorf("a bare sudo remains:\n%s", cmd)
	}
	// the secret is never on a stream a `sudo -S` reads: sudo answers a
	// rejected password by reading the next line as a second attempt
	for _, stmt := range strings.Split(cmd, ";") {
		if strings.Contains(stmt, "sudo -S") && strings.Contains(stmt, "FLEETDESK_SECRET") {
			t.Errorf("a sudo -S shares a stream with the secret: %s", stmt)
		}
	}
	got, _ := io.ReadAll(r)
	if string(got) != pw+"\n"+secret+"\n" {
		t.Errorf("stdin = %q", got)
	}
}

func TestRewriteSudoWithSecret_NoPrefix(t *testing.T) {
	sm := managerWithSudo(t, 0, "pw")
	cmd, r, err := sm.RewriteSudoWithSecret(0, "", []byte("s"), "TAG", "true")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Erase()
	if strings.Contains(cmd, "{ ") && strings.Contains(cmd, "} || exit $?") {
		t.Errorf("no prefix, no prefix block:\n%s", cmd)
	}
	if !strings.HasSuffix(cmd, `sh -c 'exec <"$0"; true' "$d/s"; rc=$?; kill $w 2>/dev/null; rm -rf "$d"; exit $rc`) {
		t.Errorf("command = %s", cmd)
	}
}

func TestRewriteSudoWithSecret_Refusals(t *testing.T) {
	sm := managerWithSudo(t, 0, "pw")
	// a host with no password held (NOPASSWD) is given an empty password line
	if cmd, r, err := sm.RewriteSudoWithSecret(1, "", []byte("s"), "TAG", "true"); err != nil {
		t.Errorf("a host holding no sudo password: %v", err)
	} else {
		got, _ := io.ReadAll(r)
		if string(got) != "\ns\n" || !strings.Contains(cmd, "sudo -S") {
			t.Errorf("stdin %q cmd %s", got, cmd)
		}
	}
	for _, bad := range []string{"a\nb", "a\rb", "a\x00b"} {
		if _, _, err := sm.RewriteSudoWithSecret(0, "", []byte(bad), "TAG", "true"); !errors.Is(err, ErrSecretDelivery) {
			t.Errorf("secret %q: %v, want ErrSecretDelivery", bad, err)
		}
	}
	for _, tag := range []string{"", "lower", "A B", "A;B", "1A", "A=B"} {
		if _, _, err := sm.RewriteSudoWithSecret(0, "", []byte("s"), tag, "true"); err == nil {
			t.Errorf("tag %q must be refused", tag)
		}
	}
	if _, _, err := sm.RewriteSudoWithSecret(0, "", []byte(strings.Repeat("x", credential.MaxLen+1)), "TAG", "true"); !errors.Is(err, credential.ErrTooLong) {
		t.Errorf("an oversized secret: %v", err)
	}
}

func TestSecretDeliveryError(t *testing.T) {
	if err := SecretDeliveryError(95); !errors.Is(err, ErrSecretDelivery) {
		t.Errorf("95: %v", err)
	}
	for _, n := range []int{0, 1, 96, 97, 98} {
		if err := SecretDeliveryError(n); err != nil {
			t.Errorf("%d: %v — another status is not a secret delivery failure", n, err)
		}
	}
}

// The rewritten command, run for real in bash with a stand-in sudo: the
// consumer receives exactly the secret's line on stdin, and a command running
// as the consumer sees neither value in its argv.
func TestRewriteSudoWithSecret_RunsEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	const pw, secret = "right", "the secret with spaces and 'quotes' $HOME"
	dir := t.TempDir()
	out := filepath.Join(dir, "stdin")
	argv := filepath.Join(dir, "argv")
	// stand-in sudo: -S reads the password line, checks it, runs the rest
	// -S reads ONE password line and checks it; a second line is the retry real
	// sudo would make, recorded so the test can fail on it. No ticket is kept
	// between invocations, as on a host with timestamp_timeout=0.
	sudo := "#!/bin/bash\n[ \"$1\" = -S ] || exit 64\nshift\nread -r p; [ \"$p\" = right ] || exit 1\nif read -r extra; then echo \"$extra\" > " + filepath.Join(dir, "second-line") + "; fi\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(sudo), 0o700); err != nil {
		t.Fatal(err)
	}
	sm := managerWithSudo(t, 0, pw)
	consumer := `printf '%s\n' "$@" > ` + argv + `; cat > ` + out
	cmd, r, err := sm.RewriteSudoWithSecret(0, "", []byte(secret), "FLEETDESK_AK", consumer)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Erase()
	c := exec.Command("bash", "-c", cmd+" 2>&1") //nolint:gosec // G204: the test runs the rewritten command against a stand-in sudo
	tmpd := filepath.Join(dir, "tmp")
	if err := os.Mkdir(tmpd, 0o700); err != nil {
		t.Fatal(err)
	}
	c.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "TMPDIR=" + tmpd}
	c.Stdin = r
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if left, _ := os.ReadDir(tmpd); len(left) != 0 {
		t.Errorf("the private directory must be removed afterwards: %v", left)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "FLEETDESK_AK="+secret+"\n" {
		t.Errorf("the consumer's stdin = %q, want the tagged secret line only (sudo consumed the password line)", got)
	}
	av, _ := os.ReadFile(argv)
	if strings.Contains(string(av), "secret") || strings.Contains(string(av), pw) {
		t.Errorf("argv carries a credential: %q", av)
	}

	if _, err := os.Stat(filepath.Join(dir, "second-line")); err == nil {
		t.Error("sudo -S saw a second stdin line: the secret shares a stream with the password prompt")
	}

	// a wrong password fails the way sudo does, without running the consumer
	sm2 := managerWithSudo(t, 0, "wrong")
	cmd2, r2, err := sm2.RewriteSudoWithSecret(0, "", []byte(secret), "FLEETDESK_AK", "echo ran > "+filepath.Join(dir, "ran"))
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Erase()
	c2 := exec.Command("bash", "-c", cmd2) //nolint:gosec // G204: as above
	c2.Env = c.Env
	c2.Stdin = r2
	if err := c2.Run(); err == nil {
		t.Error("a wrong sudo password must fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); err == nil {
		t.Error("the consumer ran despite sudo refusing")
	}

	// a rejected password must not let the secret reach sudo as a second attempt
	_ = os.Remove(filepath.Join(dir, "second-line"))
	cmdB, rB, err := sm2.RewriteSudoWithSecret(0, "", []byte(secret), "FLEETDESK_AK", "true")
	if err != nil {
		t.Fatal(err)
	}
	defer rB.Erase()
	cB := exec.Command("bash", "-c", cmdB) //nolint:gosec // G204: as above
	cB.Env = c.Env
	cB.Stdin = rB
	_ = cB.Run()
	if b, err := os.ReadFile(filepath.Join(dir, "second-line")); err == nil {
		t.Errorf("after a rejected password sudo -S read the secret line as a retry: %q", b)
	}

	// a stdin with no secret line: exit 95, mapped to ErrSecretDelivery
	c3 := exec.Command("bash", "-c", cmd) //nolint:gosec // G204: as above
	c3.Env = c.Env
	c3.Stdin = strings.NewReader("right\n") // the password line, then EOF
	err = c3.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 95 {
		t.Errorf("want exit 95, got %v", err)
	}
}

func TestRegisterHelper_Constraints(t *testing.T) {
	if strings.Contains(rhsmRegisterHelper, "'") {
		t.Error("the helper sits in a single-quoted shell word and must contain no single quote")
	}
	if strings.Contains(rhsmRegisterHelper, "%") {
		t.Error("the helper is never formatted and must contain no percent sign")
	}
	// the registration process's argv contains the helper text; none of it may
	// contain a recognisable piece of any credential the tests look for
	for _, w := range t116Windows("stub-secret"+"-value", 8) {
		if strings.Contains(rhsmRegisterHelper+RegisterConsumer("org"), w) {
			t.Errorf("the helper and consumer text contain %q", w)
		}
	}
	if !strings.Contains(rhsmRegisterHelper, "RegisterWithActivationKeys") || !strings.Contains(rhsmRegisterHelper, "FLEETDESK_AK=") {
		t.Error("the helper must call RegisterWithActivationKeys and read the tagged key line")
	}
	if strings.Contains(RegisterConsumer("o"), "subscription-manager") || strings.Contains(RegisterConsumer("o"), "--activationkey") {
		t.Error("registration never falls back to the CLI")
	}
	if RegisterTag != "FLEETDESK_AK" {
		t.Errorf("tag = %q", RegisterTag)
	}
}

func t116Windows(s string, n int) []string {
	var w []string
	for i := 0; i+n <= len(s); i++ {
		w = append(w, s[i:i+n])
	}
	return w
}

func TestRegisterConsumer_ShellSyntax(t *testing.T) {
	for _, org := range []string{"Fluxys", "o'rg", `o"rg $x`} {
		if out, err := exec.Command("bash", "-n", "-c", RegisterConsumer(org)).CombinedOutput(); err != nil { //nolint:gosec // G204: syntax check of the generated command
			t.Errorf("org %q: bash -n: %v\n%s", org, err, out)
		}
	}
	if !strings.Contains(RegisterConsumer("o'rg"), `'o'\''rg'`) {
		t.Errorf("org id is not quoted as one word: %s", RegisterConsumer("o'rg"))
	}
	// exit 98 guards: RHEL < 8 and no python3
	c := RegisterConsumer("o")
	if !strings.Contains(c, "exit 98") || !strings.Contains(c, "exit 94") {
		t.Errorf("want the version guard (98) and the D-Bus-binding guard (94):\n%s", c)
	}
	if !strings.Contains(c, "/usr/libexec/platform-python") {
		t.Errorf("a stock RHEL 8 has its D-Bus binding only under platform-python:\n%s", c)
	}
}

func TestRegisterPrefix(t *testing.T) {
	if RegisterPrefix("") != "" {
		t.Error("the CDN has no prefix")
	}
	p := RegisterPrefix("sat.example.com")
	for _, want := range []string{"sudo subscription-manager clean && ", "sudo dnf install -y 'http://sat.example.com/pub/katello-ca-consumer-latest.noarch.rpm' --disablerepo='*'"} {
		if !strings.Contains(p, want) {
			t.Errorf("prefix lacks %q: %s", want, p)
		}
	}
	if strings.Contains(p, "register") {
		t.Errorf("the prefix must not register; the D-Bus helper does: %s", p)
	}
	if !strings.Contains(RegisterPrefix("a'b"), `a'\''b`) {
		t.Errorf("satellite URL not quoted: %s", RegisterPrefix("a'b"))
	}
}

// The helper, run under python3 with a fake dbus module: it reads the key from
// stdin only, passes it to RegisterWithActivationKeys with the org, forces,
// never prints it, and reports a refusal without it.
func TestRegisterHelper_RunsAgainstAFakeDBus(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dbus"), 0o700); err != nil {
		t.Fatal(err)
	}
	logf := filepath.Join(dir, "calls.log")
	fake := `
import os
class DBusException(Exception):
    def get_dbus_name(self): return "com.redhat.RHSM1.Error"
    def get_dbus_message(self): return "refused"
class _Obj:
    def __init__(self, kind): self.kind = kind
class SystemBus:
    def get_object(self, name, path): return _Obj("server")
class Connection:
    def __init__(self, addr): self.addr = addr
    def get_object(self, name, path): return _Obj("register")
class Interface:
    def __init__(self, obj, iface): self.obj = obj
    def Start(self, locale):
        open(os.environ["CALLS"], "a").write("Start\n"); return "unix:fake"
    def Stop(self, locale):
        open(os.environ["CALLS"], "a").write("Stop\n"); return True
    def RegisterWithActivationKeys(self, org, keys, options, conn_options, locale, timeout=0):
        open(os.environ["CALLS"], "a").write("Register org=%s nkeys=%d force=%s keylen=%d\n" % (org, len(keys), options.get("force"), len(keys[0])))
        if os.environ.get("REFUSE"): raise DBusException("no")
        return '{"name": "host1", "uuid": "u-1"}'
`
	if err := os.WriteFile(filepath.Join(dir, "dbus", "__init__.py"), []byte(fake), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dbus", "connection.py"), []byte("from dbus import Connection\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(stdin string, extra ...string) (string, int) {
		c := exec.Command(py, "-c", rhsmRegisterHelper, "my-org")
		c.Env = append([]string{"PYTHONPATH=" + dir, "CALLS=" + logf, "PATH=/usr/bin:/bin"}, extra...)
		c.Stdin = strings.NewReader(stdin)
		b, err := c.CombinedOutput()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return string(b), code
	}

	// the password line (sudo asked for none) precedes the key line
	out, code := run("not-this-line\nFLEETDESK_AK=ak-123456\n")
	if code != 0 || !strings.Contains(out, "Registered host1 u-1") {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, "ak-123456") {
		t.Errorf("the key was printed: %s", out)
	}
	calls, _ := os.ReadFile(logf)
	if !strings.Contains(string(calls), "Register org=my-org nkeys=1 force=True keylen=9") || !strings.Contains(string(calls), "Stop") {
		t.Errorf("calls:\n%s", calls)
	}

	// refusal: exit 1, the D-Bus error and no key
	out, code = run("FLEETDESK_AK=ak-123456\n", "REFUSE=1")
	if code != 1 || !strings.Contains(out, "registration failed: com.redhat.RHSM1.Error") || strings.Contains(out, "ak-123456") || strings.Contains(out, "refused") {
		t.Errorf("refusal: exit %d: %s", code, out)
	}
	// no key line: exit 2
	if out, code = run("only a password\n"); code != 2 {
		t.Errorf("no key: exit %d: %s", code, out)
	}
	// no D-Bus binding: exit 98 (a dbus package that cannot be imported)
	none := t.TempDir()
	if err := os.MkdirAll(filepath.Join(none, "dbus"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(none, "dbus", "__init__.py"), []byte("raise ImportError(\"no binding\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(py, "-c", rhsmRegisterHelper, "o") //nolint:gosec // G204: as above
	c.Env = []string{"PYTHONPATH=" + none, "PATH=/usr/bin:/bin"}
	c.Stdin = strings.NewReader("FLEETDESK_AK=x\n")
	out98, err := c.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != RegisterExitNoDBus {
		t.Errorf("want exit %d without a D-Bus binding, got %v: %s", RegisterExitNoDBus, err, out98)
	}
}

// The org ID is data, not code: whatever it holds reaches python3 as one
// argument, through both layers of quoting (the consumer's and sh -c's), and
// the helper is started isolated (-I) so root never imports from the SSH
// user's working directory.
func TestRegisterConsumer_OrgIDIsOneLiteralArgument(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	pyStub := "#!/bin/bash\nprintf '%s\\0' \"$@\" > " + argvFile + "\n"
	sudoStub := "#!/bin/bash\n[ \"$1\" = -S ] || exit 64\nshift\nread -r p\nexec \"$@\"\n"
	for name, body := range map[string]string{"python3": pyStub, "sudo": sudoStub} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, org := range []string{"Fluxys", `o'rg; touch ` + dir + `/pwned; echo '`, "$(touch " + dir + "/pwned2)", "`id`", `a"b\c`, "-oProxyCommand=x"} {
		sm := managerWithSudo(t, 0, "pw")
		consumer := "py=python3; " + strings.TrimPrefix(RegisterConsumer(org), registerPreflight)
		if consumer == RegisterConsumer(org) {
			t.Fatal("the preflight must be a prefix of the consumer")
		}
		cmd, r, err := sm.RewriteSudoWithSecret(0, "", []byte("k"), RegisterTag, consumer)
		if err != nil {
			t.Fatal(err)
		}
		c := exec.Command("bash", "-c", cmd) //nolint:gosec // G204: the test runs the generated command against stubs
		c.Env = []string{"PATH=" + dir + ":/usr/bin:/bin"}
		c.Stdin = r
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("org %q: %v\n%s", org, err, out)
		}
		r.Erase()
		raw, _ := os.ReadFile(argvFile)
		args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
		if len(args) != 4 || args[0] != "-I" || args[1] != "-c" || args[2] != rhsmRegisterHelper || args[3] != org {
			t.Errorf("org %q: python3 argv = %q", org, args)
		}
		for _, f := range []string{"pwned", "pwned2"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
				t.Errorf("org %q: injected command ran (%s)", org, f)
			}
		}
	}
}
