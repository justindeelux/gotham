---
title: Getting started
---

# Getting started

Gotham is a self-hosted platform-as-a-service: a Go control plane, a node agent
on every server, and a web console for deploying applications, databases and
services. You install it on your own Linux hosts; nothing is hosted for you.

If the control plane is not installed yet, start with [Install](/guide/install).
This page picks up where the installer ends: signing in and adding your first
remote node.

## First login

On a fresh instance the first account becomes the owner. That
account is created by the installer, or by hand on the control plane host when
the install ran non-interactively without an email:

```sh
sudo -u gotham /var/lib/gotham/bin/gotham admin create --email ops@example.com
```

The command prompts for the password with hidden input. `--password-stdin` reads
a single line from a pipe (for scripts), `--generate-password` prints a random
one once, and `--force` adds a second account deliberately — without it the CLI
refuses once an account exists. `gotham admin exists` prints
`admin-exists: true|false` for scripting.

Once an account exists, self-registration is closed; additional people join
through single-use invite links created on **Users**
(see [First login](/guide/first-login)).

Open the URL the installer printed (the panel listens on port 8000 by default)
and sign in with email and password. Two more sign-in paths exist:

- **GitHub OAuth.** Available when the instance has GitHub OAuth configured with
  `GOTHAM_OAUTH_GITHUB_CLIENT_ID`, `GOTHAM_OAUTH_GITHUB_CLIENT_SECRET` and a
  redirect URL registered with the GitHub OAuth app. It is the only OAuth
  provider.
- **Two-factor authentication.** Optional per account: enable TOTP in
  **Profile → Two-factor authentication**, scan the QR code with an
  authenticator app, and store the recovery codes — they are shown once and each
  works once. After that, signing in asks for the password and then a code.
  Lost the authenticator? `gotham admin reset-password --email ops@example.com`
  replaces the password, revokes every session and clears two-factor
  authentication. Access tokens stop working immediately.

The first account holds the owner role, which the instance-wide surfaces
(instance settings, updates, DNS providers, node-wide proxy sync, servers, Git
connections and the host tools) check. Members only see the projects the owner
grants; they work inside them, with no per-project role. See
[Members and access](/using/members-access).

## Add a node

The installer already registers the control-plane host as the `localhost` node,
so you normally start with one node. Add further nodes from **Servers → Add
server**; the wizard has four steps.

1. **Connect.** Give the node a unique name, its IP address or hostname, the SSH
   port, and the SSH user. Authenticate with a private key (new or existing) or a
   password, and trust the host key. The credential is encrypted before it is
   stored and is never returned by the API. Changing the address, user or
   credential later resets the pinned host key and returns the node to pending.
2. **Validate.** The control plane probes the node over SSH for Docker, CPU, RAM
   and disk. Probes run for at most 15 seconds. If Docker is missing, the wizard
   offers to install it before you continue; the other failures must be fixed on
   the node (or by choosing a larger one) and re-checked.
3. **Install.** Copy the generated block into an SSH session on the node. It
   copies the control plane's public CA certificate, clones the installer, and
   runs the agent installer with the control-plane address, a node id and
   `--ca`. The agent registers, receives its node certificate, and the page
   polls every 5 seconds until the first heartbeat flips the status to ready.
4. **Finish.** The node is listed with its detected memory and disk; open it for
   metrics, containers, the host terminal and the file manager. Day-to-day node
   operations are covered in [Nodes and containers](/operating/nodes-containers).

### What the enrollment token is for

A node's first registration has no certificate yet, so the control plane gates
it with a one-time enrollment token. The control plane generates one (unless the
operator sets `GOTHAM_GRPC_ENROLLMENT_TOKEN`), and the installer passes it to the
agent with `--enrollment-token-file` or `GOTHAM_AGENT_ENROLLMENT_TOKEN`. Only the
first registration needs it; after that the agent authenticates with its own
certificate and key. Without a token, first enrollment is refused unless the
operator explicitly enabled open enrollment — never leave a production control
plane in that state.

### Agent health

The agent runs a small liveness endpoint at `/healthz`, bound to loopback by
default (`GOTHAM_AGENT_HEALTH_ADDR`, port 8001). It reports process liveness only
— it deliberately does not check the control-plane connection, so an update is
not rolled back just because the control plane is briefly unreachable. A node is
reported offline when no heartbeat arrives for 30 seconds; the agent sends one
every 10 seconds.

Next: deploy something in [Projects and environments](/using/projects-environments),
keep the installation current in [Updates](/guide/updates), and use
[Troubleshooting](/guide/troubleshooting) when a node or the panel misbehaves.
