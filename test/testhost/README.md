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
- A committed fleet file (`fleet.yaml`) pointing at the container, so
  verification never depends on a fleet file someone typed by hand.

## Use it

```
make testhost-up                          # build/start, prints how to reach it
go test -tags testhost -run TestTestHost -v .
make testhost-regen-host-key              # rotate the host key in place
make testhost-down                        # stop, remove, wipe generated keys
```

The client keypair used for key auth is generated fresh on every
`testhost-up` into `.keys/` (gitignored) and is never committed.

## What it deliberately does not cover

- **systemd.** Running it as PID 1 needs a privileged container and breaks
  on some hosts. Nothing in M2 exercises systemd — that is SSH auth,
  credential delivery, and file permissions, all sshd/kernel behaviour. The
  Services, Processes, and Security & Audit views need a real RHEL host and
  belong to the VM tier.
- **Real `podman`, `dnf`, or `subscription-manager` registration.** The
  fixture ships the inert stub above so there is a process to observe; it
  does not attempt real registration.
- **CI.** This runs locally, behind the `testhost` Go build tag, so
  `go test ./...`, `make test`, `make verify`, and `make regression` never
  start a container.
