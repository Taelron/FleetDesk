#!/bin/bash
# TAE-98 test host entrypoint: generate host keys if absent, install the
# per-lifecycle public key bind-mounted in by `make testhost-up`, then exec
# sshd as PID 1 (absolute path, -D -e) so a later SIGHUP re-execs it and
# picks up regenerated host keys (`make testhost-regen-host-key`, AC 5).
set -euo pipefail

mkdir -p /run/sshd
ssh-keygen -A

pubkey=/run/testhost/client_ed25519.pub
if [ -f "$pubkey" ]; then
    install -d -m 0700 -o testuser1 -g testuser1 /home/testuser1/.ssh
    install -m 0600 -o testuser1 -g testuser1 "$pubkey" /home/testuser1/.ssh/authorized_keys
fi

exec /usr/sbin/sshd -D -e
