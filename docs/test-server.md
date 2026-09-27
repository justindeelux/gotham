# Shared Test Server

This is the long-lived test box used by both the project owner and coding
agents for manual verification (real-machine SSH validation, agent
registration, end-to-end smoke tests). It is an **all-in-one** host: the
control plane, Postgres, Redis, and a node agent all run on the same machine.

## Connection

- SSH alias: `gotham` (see local `~/.ssh/config` on each operator machine).
- OS: Ubuntu 22.04, root access.
- Specs at provisioning time: 4 vCPU, 7 GB RAM, ~68 GB free disk.
- Docker Engine + compose plugin installed (see below).

```sh
ssh gotham
```

New operators: add a `Host gotham` entry (hostname, port, user, identity file)
to your own `~/.ssh/config`. Never commit connection details or private keys
to this repository.

## Provisioning (done once, 2026-09-27)

1. Waited for first-boot `unattended-upgrades` to release the apt lock
   (do NOT kill it mid-upgrade).
2. Installed Docker from the official Docker apt repository:
   `docker-ce`, `docker-ce-cli`, `containerd.io`,
   `docker-buildx-plugin`, `docker-compose-plugin`; enabled via systemd.
3. The kernel was upgraded by the auto-updater; reboot once when convenient.

## All-in-one test workflow

Run on the server after cloning the repository:

```sh
git clone https://github.com/justindeelux/gotham.git && cd gotham
docker compose -f deploy/compose.dev.yml up -d
make migrate
make build
./bin/gotham serve
# CP serves the UI/API on :8000 and the agent gRPC gateway on :9442.
```

In a second shell on the same server:

```sh
GOTHAM_AGENT_CP_ADDR=localhost:9442 \
GOTHAM_AGENT_NODE_ID=test-node-1 \
GOTHAM_AGENT_CERT_DIR=./data/agent \
./bin/gotham-agent serve
```

Expected: the agent registers, the control plane shows the server as `ready`
with live CPU/RAM/disk metrics, and heartbeats arrive every 10s.

## Notes

- Development mode: when there is no CA (empty `GOTHAM_CA_DIR`), the control
  plane dials agents over plaintext and the agent serves its DockerService
  without TLS. As soon as a CA exists, registration issues certificates and
  both sides use mTLS.
- Go toolchain: install a Go release matching `go.mod` before `make build`.
- Ports used on this host: 8000 (CP HTTP), 9442 (CP gRPC), 9443 (agent
  DockerService), 5432 (Postgres), 6379 (Redis).
- This box holds no production data; wiping the dev database or reinstalling
  the agent is always acceptable.
