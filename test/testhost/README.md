# Test host container (TAE-98)

M2's exit criterion is written against "a container running sshd." This is
that container: a disposable, repo-owned fixture providing `sshd` and `sudo`
so every M2 issue verifies against the same target instead of one improvised
by whoever runs it.

## What it provides

- `sshd` reachable on `127.0.0.1:2222`, with both password and public-key
  authentication enabled.
- Two accounts, `testuser1` and `testuser2`, both sudo-capable via the
  container's `%wheel` group, each with its own fixed sudo password —
  `sudo` always demands a password, and the two accounts' passwords differ.
  `testuser1` accepts both password and key auth; `testuser2` is
  password-only.
- A host key that can be regenerated on demand, in place, while the
  container keeps running (`make testhost-regen-host-key`).
- An inert `subscription-manager` stub on `PATH`: it drains stdin (or a 20s
  cap), then holds a couple seconds more regardless of how it was called,
  before exiting 0. It registers nothing.
- A stub RHSM D-Bus (TAE-116, L2): `python3` with a stand-in `dbus` module
  that replaces the real dbus-python in the image. It implements only the
  calls FleetDesk's registration helper makes; `RegisterWithActivationKeys`
  holds for three seconds (so the registering process is resident long enough
  to be observed in `ps`), appends `org=<org> key_len=<n>` to
  `/run/testhost/register.log` and returns a consumer. It never stores a key.
- An inert `dnf` stub on `PATH` that answers `check-update` at once with
  "nothing to update". The real one spends tens of CPU-bound seconds failing
  to find repositories, which kept hosts "connecting..." past every
  acceptance-test wait. Any other `dnf` command fails.
- A committed fleet file (`fleet.yaml`) pointing at the container, so
  verification never depends on a fleet file someone typed by hand.

## TAE-116 targets

```
make testhost-m2                 # every TestTAE116* acceptance test -> test/testhost/tae116.out (about 12 min)
make testhost-exit-criterion     # H1-H5 only: the M2 exit criterion
make testhost-register-ps        # B4 evidence: registration with ps/proc sampled; paste into the PR
make keyvault-resolve VAULT=<v> SUBSCRIPTION=<s> NAME=<n>   # L1, the one human step: a real Key Vault, the real az
```

None of them joins `make regression` (macOS CI has no Podman). `test/azstub/az`
is the stand-in for `az` on `PATH` that the secret-resolution tests use.

## Use it

```
make testhost-up                          # build/start, prints how to reach it
make testhost-test                        # run every testhost-tagged acceptance test
make testhost-regen-host-key              # rotate the host key in place
make testhost-down                        # stop, remove, wipe generated keys and isolated HOME
```

The client keypair used for key auth is generated fresh on every
`testhost-up` into `.keys/` (gitignored) and is never committed.

TAE-20 (sudo password delivered over stdin, never in the command string)
adds a few narrower targets:

```
make testhost-sudo-ps               # AC2 evidence: the named resident-command test, -v output to paste into a PR
make testhost-verify-sudo-stdin     # AC2 evidence: every TAE-20 acceptance test, -v output to paste into a PR
make testhost-run                   # launch the TUI on the fixture fleet under an isolated HOME
make testhost-log-scan              # AC6: that session's logs ran the sudo path and carry no fixture password
```

`testhost-run` sets `HOME` to `test/testhost/.home` for the launched
process, so both the config it reads (`ConfigPath()`) and the logs it
writes (`LogDir()`) are relocated there — `~/.config/fleetdesk` is never
touched, and nothing has to be restored afterward. `.home/` is gitignored
beside `.keys/` and is wiped by `testhost-down`.

## What it deliberately does not cover

- **systemd.** Running it as PID 1 needs a privileged container and breaks
  on some hosts. Nothing in M2 exercises systemd — that is SSH auth,
  credential delivery, and file permissions, all sshd/kernel behaviour. The
  Services, Processes, and Security & Audit views need a real RHEL host and
  belong to the VM tier.
- **A real RHSM.** D-Bus is a stub (L2): the fixture measures FleetDesk's
  transport — the command string, the shell, the sudo relay, the helper's
  argv, stdin — not the real `RegisterWithActivationKeys` call, which is
  reviewed but not executed until TAE-99's VM tier.
- **Real `podman`, `dnf`, or `subscription-manager` registration.** The
  fixture ships the inert stubs above so there is a process to observe; it
  does not attempt real registration or package management.
- **CI.** This runs locally, behind the `testhost` Go build tag, so
  `go test ./...`, `make test`, `make verify`, and `make regression` never
  start a container.
