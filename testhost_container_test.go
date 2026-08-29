//go:build testhost

package main_test

// Acceptance tests for TAE-98: Test host container -- sshd, sudo, and a
// regenerable host key. Written from the issue's ten acceptance criteria
// only -- no plan comment, no implementation, no Makefile or container work
// (existing or otherwise) was read to produce these.
//
// The issue leaves several concrete details unspecified (Make target names,
// fixture paths, account names/passwords, key locations). Those are fixed
// here as the interface this issue's deliverable must meet; a disputed
// choice is Gaetan's to settle, not the implementer's to work around.
//
// Assumed interface:
//
//	Make targets:   testhost-up, testhost-down, testhost-regen-host-key
//	Container name: fleetdesk-testhost
//	Fixture root:   test/testhost/
//	  Containerfile   test/testhost/Containerfile   (base image pinned by digest)
//	  Fleet file      test/testhost/fleet.yaml       (type: vm, checked in)
//	  Documentation   test/testhost/README.md        (purpose + explicit non-goals)
//	  Generated keys  test/testhost/.keys/ (gitignored; written by testhost-up)
//	    private key   test/testhost/.keys/client_ed25519
//	    public key    test/testhost/.keys/client_ed25519.pub
//	Accounts (both provisioned for sudo, each with its own sudo password):
//	  testuser1 / th98-testuser1-pw
//	  testuser2 / th98-testuser2-pw
//	`testhost-up` prints a usable `ssh -p <port> user@<host>`-shaped line;
//	the port and host it prints must agree with the committed fleet file, so
//	verification never depends on a connection string a person typed by hand.
//
// This is the manual, local integration tier the SPEC describes -- it is
// gated behind the `testhost` build tag precisely so `go test ./...`,
// `make test`, `make verify` and `make regression` never try to start a
// container. Run explicitly, with Podman available:
//
//	go test -tags testhost -run TestTestHost -v .

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

const (
	containerName = "fleetdesk-testhost"

	fixtureDir        = "test/testhost"
	containerfilePath = fixtureDir + "/Containerfile"
	fleetFilePath     = fixtureDir + "/fleet.yaml"
	readmePath        = fixtureDir + "/README.md"
	keysDir           = fixtureDir + "/.keys"
	clientKeyPath     = keysDir + "/client_ed25519"
	clientPubKeyPath  = clientKeyPath + ".pub"

	testUser1         = "testuser1"
	testUser1Password = "th98-testuser1-pw"
	testUser2         = "testuser2"
	testUser2Password = "th98-testuser2-pw"
)

type testhostInfo struct {
	Host string
	Port string
}

var (
	sharedInfo  *testhostInfo
	sharedUpOut string
	sharedUpErr error
)

// TestMain starts the fixture once for the whole file and tears it down
// once at the end, rather than every test paying container-startup cost.
// If startup fails, sharedUpErr carries the reason and every
// container-dependent test fails through requireTesthost with that reason
// attached, instead of each re-deriving its own "why" independently.
func TestMain(m *testing.M) {
	sharedUpOut, sharedUpErr = runMake("testhost-up")
	if sharedUpErr == nil {
		sharedInfo, sharedUpErr = parseConnInfo(sharedUpOut)
	}
	code := m.Run()
	if out, err := runMake("testhost-down"); err != nil {
		fmt.Fprintf(os.Stderr, "testhost_container_test.go: testhost-down cleanup failed: %v\n%s\n", err, out)
	}
	os.Exit(code)
}

func runMake(target string) (string, error) {
	out, err := exec.Command("make", target).CombinedOutput()
	return string(out), err
}

var (
	connPortRe = regexp.MustCompile(`-p\s+(\d+)`)
	connHostRe = regexp.MustCompile(`@([A-Za-z0-9_.:-]+)`)
)

// parseConnInfo pulls host/port out of an `ssh -p <port> user@<host>`-shaped
// line, deliberately tolerant of exact wording -- the AC is "prints how to
// reach it", not a fixed message format.
func parseConnInfo(out string) (*testhostInfo, error) {
	pm := connPortRe.FindStringSubmatch(out)
	hm := connHostRe.FindStringSubmatch(out)
	if pm == nil || hm == nil {
		return nil, fmt.Errorf("could not find an `ssh -p <port> user@<host>`-shaped connection line in testhost-up output:\n%s", out)
	}
	return &testhostInfo{Host: hm[1], Port: pm[1]}, nil
}

func requireTesthost(t *testing.T) *testhostInfo {
	t.Helper()
	if sharedUpErr != nil {
		t.Fatalf("testhost-up did not succeed, so this container-dependent check cannot run (TAE-98 AC1): %v\n%s", sharedUpErr, sharedUpOut)
	}
	if sharedInfo == nil {
		t.Fatalf("no connection info was parsed from testhost-up output (TAE-98 AC1)")
	}
	return sharedInfo
}

func mustDial(t *testing.T, info *testhostInfo, user string, auth ssh.AuthMethod) *ssh.Client {
	t.Helper()
	client, err := dialSSH(info, user, auth, 10*time.Second)
	if err != nil {
		t.Fatalf("dial %s@%s:%s: %v", user, info.Host, info.Port, err)
	}
	return client
}

// dialSSH ignores the host key: TOFU/known_hosts verification is TAE-22's
// concern, not this fixture's.
func dialSSH(info *testhostInfo, user string, auth ssh.AuthMethod, timeout time.Duration) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	return ssh.Dial("tcp", net.JoinHostPort(info.Host, info.Port), cfg)
}

func runRemote(client *ssh.Client, cmd string) (string, error) {
	return runRemoteWithStdin(client, cmd, "")
}

func runRemoteWithStdin(client *ssh.Client, cmd, stdin string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer func() { _ = session.Close() }()
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	out, err := session.CombinedOutput(cmd)
	return string(out), err
}

func containerID(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("podman", "inspect", "-f", "{{.Id}}", containerName).Output()
	if err != nil {
		t.Fatalf("podman inspect %s: %v", containerName, err)
	}
	return strings.TrimSpace(string(out))
}

func sshKeyscanEd25519(t *testing.T, info *testhostInfo) string {
	t.Helper()
	out, err := exec.Command("ssh-keyscan", "-t", "ed25519", "-p", info.Port, info.Host).Output()
	if err != nil {
		t.Fatalf("ssh-keyscan -t ed25519 -p %s %s: %v", info.Port, info.Host, err)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("ssh-keyscan returned no ed25519 host key for %s:%s", info.Host, info.Port)
	}
	return strings.Join(lines, "\n")
}

func firstFleetHost(fleet config.Fleet) (config.HostEntry, bool) {
	for _, g := range fleet.Groups {
		if len(g.Hosts) > 0 {
			return g.Hosts[0], true
		}
	}
	if len(fleet.Hosts) > 0 {
		return fleet.Hosts[0], true
	}
	return config.HostEntry{}, false
}

// --- AC: a fleet file for it lives in the repository ---

func TestTestHostFleetFileExistsAndParses(t *testing.T) {
	fleet, err := config.ParseFleetFile(fleetFilePath)
	if err != nil {
		t.Fatalf("expected a fleet file at %s for the test host, so verification does not depend on one written by hand (TAE-98 AC2): %v", fleetFilePath, err)
	}
	if fleet.Type != "vm" {
		t.Errorf("expected fleet type \"vm\" in %s, got %q (TAE-98 AC2)", fleetFilePath, fleet.Type)
	}
	entry, ok := firstFleetHost(fleet)
	if !ok {
		t.Fatalf("expected %s to define at least one host (TAE-98 AC2)", fleetFilePath)
	}
	if entry.Hostname == "" {
		t.Errorf("expected the fleet file's host entry to set a hostname (TAE-98 AC2)")
	}
	port := entry.Port
	if port == 0 {
		port = fleet.Defaults.Port
	}
	if port == 0 {
		t.Errorf("expected the fleet file (directly or via defaults) to set a non-zero port (TAE-98 AC2)")
	}
}

// --- AC: base image pinned by digest ---

func TestTestHostContainerfileBaseImagePinnedByDigest(t *testing.T) {
	data, err := os.ReadFile(containerfilePath)
	if err != nil {
		t.Fatalf("read %s (TAE-98 AC7): %v", containerfilePath, err)
	}
	fromRe := regexp.MustCompile(`(?mi)^\s*FROM\s+(\S+)`)
	matches := fromRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatalf("expected at least one FROM line in %s (TAE-98 AC7)", containerfilePath)
	}
	digestRe := regexp.MustCompile(`@sha256:[0-9a-fA-F]{64}$`)
	for _, m := range matches {
		ref := m[1]
		if strings.EqualFold(ref, "scratch") {
			continue
		}
		if !digestRe.MatchString(ref) {
			t.Errorf("expected FROM %q in %s to pin the base image by digest (@sha256:<64 hex>), not a moving tag -- a fixture that changes underneath you produces failures nobody can reproduce (TAE-98 AC7)", ref, containerfilePath)
		}
	}
}

// --- AC: no key material committed ---

func TestTestHostNoKeyMaterialCommitted(t *testing.T) {
	out, err := exec.Command("git", "grep", "-lI",
		"-e", "BEGIN OPENSSH PRIVATE KEY",
		"-e", "BEGIN RSA PRIVATE KEY",
		"-e", "BEGIN EC PRIVATE KEY",
		"-e", "BEGIN PRIVATE KEY",
	).CombinedOutput()
	if err == nil {
		t.Errorf("expected no committed private key material (TAE-98 AC8); git grep found matches in:\n%s", out)
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("git grep for private key material: %v\n%s", err, out)
	}

	if err := exec.Command("git", "check-ignore", "-q", clientKeyPath).Run(); err != nil {
		t.Errorf("expected %s to be gitignored so a generated key can never be accidentally committed (TAE-98 AC8): git check-ignore did not confirm it: %v", clientKeyPath, err)
	}
}

// --- AC: README/CLAUDE.md documents purpose and explicit non-goals ---

func TestTestHostDocumentationDescribesPurposeAndScope(t *testing.T) {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("expected documentation describing the container's purpose and scope at %s (TAE-98 AC10): %v", readmePath, err)
	}
	lower := strings.ToLower(string(data))
	for _, must := range []string{"sshd", "sudo", "host key"} {
		if !strings.Contains(lower, must) {
			t.Errorf("expected %s to describe %q as part of the container's purpose (TAE-98 AC10)", readmePath, must)
		}
	}
	if !strings.Contains(lower, "systemd") {
		t.Errorf("expected %s to name systemd as deliberately out of scope (TAE-98 AC10)", readmePath)
	}
	if !strings.Contains(lower, "does not") && !strings.Contains(lower, "out of scope") && !strings.Contains(lower, "not cover") {
		t.Errorf("expected %s to state what the container deliberately does not cover, not just what it does (TAE-98 AC10)", readmePath)
	}
}

// --- AC: a Make target starts the container and prints how to reach it ---

func TestTestHostUpPrintsConnectionInfoMatchingFleetFile(t *testing.T) {
	info := requireTesthost(t)

	fleet, err := config.ParseFleetFile(fleetFilePath)
	if err != nil {
		t.Fatalf("parse committed fleet file %s (TAE-98 AC2): %v", fleetFilePath, err)
	}
	entry, ok := firstFleetHost(fleet)
	if !ok {
		t.Fatalf("fleet file %s defines no host (TAE-98 AC2)", fleetFilePath)
	}
	port := entry.Port
	if port == 0 {
		port = fleet.Defaults.Port
	}
	if strconv.Itoa(port) != info.Port {
		t.Errorf("testhost-up printed port %s but the committed fleet file %s says port %d (TAE-98 AC1/AC2) -- verification should not depend on a hand-typed connection string", info.Port, fleetFilePath, port)
	}
}

// --- AC: password and key authentication both work, independently ---

func TestTestHostPasswordAuthWorks(t *testing.T) {
	info := requireTesthost(t)
	client := mustDial(t, info, testUser1, ssh.Password(testUser1Password))
	defer func() { _ = client.Close() }()

	out, err := runRemote(client, "whoami")
	if err != nil {
		t.Fatalf("run `whoami` over a password-authenticated session (TAE-98 AC3): %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != testUser1 {
		t.Errorf("expected whoami to report %s, got %q (TAE-98 AC3)", testUser1, out)
	}
}

func TestTestHostPublicKeyAuthWorks(t *testing.T) {
	info := requireTesthost(t)

	keyBytes, err := os.ReadFile(clientKeyPath)
	if err != nil {
		t.Fatalf("read the generated client private key at %s (TAE-98 AC3/AC8): %v", clientKeyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		t.Fatalf("parse generated client private key %s: %v", clientKeyPath, err)
	}

	client := mustDial(t, info, testUser1, ssh.PublicKeys(signer))
	defer func() { _ = client.Close() }()

	out, err := runRemote(client, "whoami")
	if err != nil {
		t.Fatalf("run `whoami` over a key-authenticated session (TAE-98 AC3): %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != testUser1 {
		t.Errorf("expected whoami to report %s, got %q (TAE-98 AC3)", testUser1, out)
	}
}

// --- AC: sudo requires a password, and it differs between the two accounts ---

func TestTestHostSudoRequiresPassword(t *testing.T) {
	info := requireTesthost(t)
	client := mustDial(t, info, testUser1, ssh.Password(testUser1Password))
	defer func() { _ = client.Close() }()

	if out, err := runRemoteWithStdin(client, "sudo -n true", ""); err == nil {
		t.Errorf("expected `sudo -n true` (no password offered) to fail for %s (TAE-98 AC4); it succeeded: %s", testUser1, out)
	}
	if out, err := runRemoteWithStdin(client, "sudo -S -p '' true", testUser1Password+"\n"); err != nil {
		t.Errorf("expected `sudo -S true` to succeed for %s given its own password (TAE-98 AC4): %v\n%s", testUser1, err, out)
	}
}

func TestTestHostSudoPasswordDiffersBetweenAccounts(t *testing.T) {
	info := requireTesthost(t)
	client := mustDial(t, info, testUser2, ssh.Password(testUser2Password))
	defer func() { _ = client.Close() }()

	if out, err := runRemoteWithStdin(client, "sudo -S -p '' true", testUser1Password+"\n"); err == nil {
		t.Errorf("expected %s's sudo password to be rejected for %s (TAE-98 AC4, TAE-89); it was accepted: %s", testUser1, testUser2, out)
	}
	if out, err := runRemoteWithStdin(client, "sudo -S -p '' true", testUser2Password+"\n"); err != nil {
		t.Errorf("expected %s's own sudo password to work for %s (TAE-98 AC4): %v\n%s", testUser2, testUser2, err, out)
	}
}

// --- AC: a command run over SSH is visible from the host, via a visible marker ---

func TestTestHostCommandRunOverSSHVisibleInProcessListFromHost(t *testing.T) {
	info := requireTesthost(t)
	client := mustDial(t, info, testUser1, ssh.Password(testUser1Password))
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("open session for marked command (TAE-98 AC6): %v", err)
	}
	defer func() { _ = session.Close() }()

	marker := fmt.Sprintf("fleetdesk_tae98_marker_%d", time.Now().UnixNano())
	if err := session.Start(fmt.Sprintf("bash -c 'exec -a %s sleep 30'", marker)); err != nil {
		t.Fatalf("start marked background command over SSH (TAE-98 AC6): %v", err)
	}
	defer func() { _ = session.Signal(ssh.SIGTERM) }()

	var psOut []byte
	found := false
	for range 20 {
		psOut, err = exec.Command("podman", "exec", containerName, "ps", "-eo", "args").CombinedOutput()
		if err == nil && strings.Contains(string(psOut), marker) {
			found = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !found {
		t.Errorf("expected `podman exec %s ps` (run from the host) to show the marked SSH-launched command %q (TAE-98 AC6); ps output:\n%s\nlast error: %v", containerName, marker, psOut, err)
	}
}

// --- AC: an inert subscription-manager stub is on PATH, reads stdin, and stays alive ---

func TestTestHostSubscriptionManagerStubInertAndObservable(t *testing.T) {
	info := requireTesthost(t)
	client := mustDial(t, info, testUser1, ssh.Password(testUser1Password))
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("open session for subscription-manager stub check (TAE-98 AC9): %v", err)
	}
	defer func() { _ = session.Close() }()

	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if err := session.Start("subscription-manager register --activationkey=fake --org=fake"); err != nil {
		t.Fatalf("expected subscription-manager on the container's PATH (TAE-98 AC9): %v", err)
	}

	var psOut []byte
	found := false
	for range 20 {
		psOut, err = exec.Command("podman", "exec", containerName, "ps", "-eo", "comm").CombinedOutput()
		if err == nil && strings.Contains(string(psOut), "subscription-manager") {
			found = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected the inert subscription-manager stub to be visible in the container's process listing while it waits on stdin (TAE-98 AC9); ps output:\n%s\nlast error: %v", psOut, err)
	}

	if err := stdin.Close(); err != nil { // EOF -- the stub should read stdin, then exit
		t.Fatalf("close stdin: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); !ok || exitErr.ExitStatus() != 0 {
				t.Errorf("expected the stub to exit cleanly once stdin closes rather than attempt real registration (TAE-98 AC9): %v", err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Errorf("subscription-manager stub did not exit within 5s of stdin closing -- expected an inert stub, not a process that hangs regardless (TAE-98 AC9)")
	}
}

// --- AC: a target regenerates the host key while the container is running ---

func TestTestHostRegenerateHostKeyChangesFingerprint(t *testing.T) {
	info := requireTesthost(t)

	before := sshKeyscanEd25519(t, info)
	idBefore := containerID(t)

	out, err := runMake("testhost-regen-host-key")
	if err != nil {
		t.Fatalf("make testhost-regen-host-key (TAE-98 AC5): %v\n%s", err, out)
	}

	after := sshKeyscanEd25519(t, info)
	if before == after {
		t.Errorf("expected the host key to change after `make testhost-regen-host-key`, so a reconnection sees a changed key (TAE-98 AC5); ssh-keyscan agrees before and after:\n%s", after)
	}

	idAfter := containerID(t)
	if idBefore != idAfter {
		t.Errorf("expected the same running container before and after key regen, not a freshly recreated one (TAE-98 AC5); id before %q, after %q", idBefore, idAfter)
	}
}

// --- AC: a Make target stops and removes the container ---

func TestTestHostDownStopsAndRemovesContainer(t *testing.T) {
	requireTesthost(t)

	out, err := runMake("testhost-down")
	if err != nil {
		t.Fatalf("make testhost-down (TAE-98 AC1): %v\n%s", err, out)
	}
	psOut, _ := exec.Command("podman", "ps", "-a", "--filter", "name=^"+containerName+"$", "--format", "{{.Names}}").Output()
	if strings.TrimSpace(string(psOut)) != "" {
		t.Errorf("expected no container named %s after testhost-down (TAE-98 AC1); podman still lists: %q", containerName, psOut)
	}

	// Restart so any test placed after this one in the file still has a
	// live container to work against.
	upOut, upErr := runMake("testhost-up")
	if upErr != nil {
		t.Fatalf("could not restart testhost after the down/up cycle: %v\n%s", upErr, upOut)
	}
	info, perr := parseConnInfo(upOut)
	if perr != nil {
		t.Fatalf("re-parse connection info after restart: %v", perr)
	}
	sharedInfo, sharedUpErr = info, nil
}

// --- AC: the keypair used for public-key auth is generated on first start, never committed ---

func TestTestHostKeypairGeneratedOnFirstStart(t *testing.T) {
	if err := os.RemoveAll(keysDir); err != nil {
		t.Fatalf("remove pre-existing %s before checking first-start generation: %v", keysDir, err)
	}
	if out, err := runMake("testhost-down"); err != nil {
		t.Logf("testhost-down before first-start check (best effort, container may already be absent): %v\n%s", err, out)
	}

	out, err := runMake("testhost-up")
	if err != nil {
		t.Fatalf("make testhost-up (TAE-98 AC1/AC8): %v\n%s", err, out)
	}
	if _, err := os.Stat(clientKeyPath); err != nil {
		t.Errorf("expected a client keypair generated at %s on first start, never committed (TAE-98 AC8): %v", clientKeyPath, err)
	}
	if _, err := os.Stat(clientPubKeyPath); err != nil {
		t.Errorf("expected the matching public key at %s (TAE-98 AC8): %v", clientPubKeyPath, err)
	}

	info, perr := parseConnInfo(out)
	if perr != nil {
		t.Fatalf("re-parse connection info after regenerating the fixture: %v", perr)
	}
	sharedInfo, sharedUpErr = info, nil
}
