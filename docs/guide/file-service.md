---
title: Remote file service
---

# Remote file service

The node agent can expose a file browser and editor to the panel, so the owner
can read, upload, edit, compress and extract files on a node without SSH.

## What you need

- The node agent running on the node (see [Add nodes](/guide/nodes)).
- The control-plane file API on, which is the default (`FEATURE_FILES=false`
  turns it off).
- The owner account, or an owner's API token with the `admin` scope.

The service is **on by default** and, in its default mode, exposes the whole
filesystem. Read the first section before rolling it out.

## Whole-filesystem mode (default)

The file service is on unless `GOTHAM_AGENT_FILES=false`, and by default it
exposes one writable root named `system` that is `/`: every file the agent
process can reach, with no dangerous or protected path excluded (including the
agent's own key and certificates). The owner accepted this risk; only the owner
can use it.

What the agent can really read or change is bounded by its Unix user. The
control-plane installer enables the file manager on its own `localhost` node by
default, with no prompt, and runs that agent as root via the
`file-manager.conf` drop-in. For a node added later, the **Add server** wizard
has an **Enable the file manager on this node** checkbox (off by default) and
passes `--file-manager` or `--no-file-manager` in the generated install command.
Run as the unprivileged `gotham-agent` user, the same root is mostly read-only.
Existing nodes that auto-update to this version expose `/` to the owner with the
permissions of their current user until `GOTHAM_AGENT_FILES=false` is set.

In this mode the application-directory guard still protects top-level app
directories of the managed volumes root (no rename/delete while a container
mounts them). `os.Root` refuses to follow an *absolute* symlink even inside `/`,
so a path through one (for example `/var/run`) cannot be entered through the
link; use the real path. Answering no after a yes runs
`chown -R gotham-agent` on `/var/lib/gotham-agent` so the certificate and key
the root agent wrote belong to the service user again.

`GOTHAM_AGENT_FILES` and `GOTHAM_AGENT_FILE_FULL_ACCESS` are switched off by
`false`, `0`, `no` or `off`; any other value leaves them on.

The choice is made when the node is installed. To change it later, edit
`GOTHAM_AGENT_FILES` in `/etc/gotham/agent.env` on that node and run
`systemctl restart gotham-agent`; the panel has no control for it. When the
service is off, the agent still registers it and every call answers
`FailedPrecondition` with `file_manager_disabled`; the API returns **409** with
`{"code":"file_manager_disabled"}`, and the panel says the file manager is
turned off on this node. A genuinely old agent that does not register the
service still answers **501** (`agent_outdated`).

## Confined mode

With `GOTHAM_AGENT_FILE_FULL_ACCESS=false`, roots are an agent-local allow-list:

- `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` — the writable root, holding the
  application bind mounts.
- `GOTHAM_AGENT_COMPOSE_ROOT` — the compose tree, browsable read-only (the next
  `ComposeUp` overwrites edits).
- `GOTHAM_AGENT_FILE_ROOTS` — colon-separated operator roots, read-only in this
  phase.

Every path is resolved and validated at startup. A root that is not absolute,
resolves into `/etc /proc /sys /dev /run /boot /usr /home /root`, Docker's data
root (the `data-root` from `/etc/docker/daemon.json`, else `/var/lib/docker`) or
the agent's own state refuses startup; so does a root that overlaps another
root. A built-in root that does not exist yet (Docker and the compose service
create those lazily) is skipped with a warning instead of aborting the agent.

Confinement itself is `os.OpenRoot`: every access is resolved relative to a
held directory descriptor, so a `..` component, an absolute path or a symlink
that leaves the tree is refused by the descriptor walk. The service must not be
exposed without the agent's CA configured, since confinement is not
authentication. Each root's identity is taken from the opened descriptor, so a
path component swapped while the roots are validated refuses startup instead of
registering the swapped directory.

A write keeps the inode when the agent user does not own the file or the file
has more than one hard link: rewriting it in place preserves its owner, mode and
mount instead of replacing it with a new inode. A hard link whose other name
lies outside the root therefore shares that rewrite, so editing the file through
the root changes the outside name too. This is an accepted risk: creating the
link already needs write access to the target, roots are operator-controlled,
and a single-link file owned by the agent user is still replaced atomically by
rename. Rename or delete the entry to break the link if the outside name must
not change.

`List` returns at most 5,000 entries per page, sorted by name. A page that has
more entries than one response can carry reports `truncated=true` and an opaque
base64 `cursor`; pass the cursor back to resume, and keep paging until
`truncated=false` — a truncated page always carries a non-empty cursor that
makes progress, so a client can never loop. The cursor is base64 because a
boundary name may hold bytes that are not valid UTF-8 (legal on Linux), which a
raw proto3 string could not carry; the displayed `name` of such an entry is
scrubbed to U+FFFD and therefore cannot be fed back to `Stat`/`Read`, but the
cursor still addresses the real bytes. A malformed cursor is rejected with
`InvalidArgument`. A single `List` scans up to 100,000 directory entries and
keeps only the page in memory; a directory larger than that fails with
`ResourceExhausted` ("directory too large") rather than returning a silently
partial listing.

## Archives (compress / extract)

`Compress` and `Extract` build and unpack `zip`, `tar` and `tar.gz` **without a
shell** (`archive/zip`, `archive/tar`, `compress/gzip` only; no `os/exec`
anywhere). Compress skips symbolic links and special files (never following a
link) and counts them in the final progress message (which the control plane
does not surface to the browser — see below); the archive is staged in a
temporary file and renamed into place, so a failed or cancelled compress never
leaves a partial archive.

Extract refuses symbolic-link, hard-link, device and FIFO entries, strips
setuid/setgid/sticky bits, and writes every entry through `os.Root`, so an
absolute name, a `..` segment or a symlinked parent cannot escape the
destination root. A file that already exists is refused unless the caller sets
`overwrite`; an existing directory is reused, never removed. Each file is
written to a temporary sibling and renamed over the target, so a failed
overwrite never truncates the existing file. Both guard the running-app
directory (an archive cannot replace a top-level application directory of the
managed volumes root), refuse a read-only root, and honour the free-space
reserve and transfer budget of the write path.

**Extract is not transactional.** A failure part-way leaves the entries already
unpacked in place (only the file being written is staged and removed), so the
caller must treat a failed extract as a partial result and clean up if needed.

Four bounds guard an archive: the **on-disk size of the archive file** extract
will read (`GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES`, default 1 GiB, checked before
any parser runs and for every format); the total uncompressed bytes
(`GOTHAM_AGENT_ARCHIVE_MAX_BYTES`, default 4 GiB); the entry count
(`GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES`, default 100000); and a **fixed** compression
ratio of **200:1** against the archive's own size (not env-configurable).
Directory entries count toward the entry cap. For a zip, the
end-of-central-directory record (and the zip64 locator/record when present) is
read from the file's tail before `zip.NewReader` runs, and the archive is
refused when its declared entry count exceeds `GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES`
or its declared central-directory size exceeds 64 MiB — so a crafted archive
declaring millions of entries is rejected before any header is allocated. An
entry name deeper than 64 segments is refused. An over-limit archive is refused
before it fills the disk. All limits are read as positive integers; an
unparseable or non-positive value silently falls back to its default (like the
other `GOTHAM_AGENT_FILE_*` limits), so a typo does not disable the cap.

The skipped-entry count a compress emits is **not surfaced to the browser**: the
agent reports it in the stream's terminal `Progress.skipped`, but the control
plane drains the stream and returns only the created archive, so the UI cannot
show it.

An archive limit is answered **413** with a distinct `archive_limit` body code,
a refused archive member (a link, device, FIFO or unsupported type) **409** with
an `archive_entry` code, and a truncated or corrupt archive body **400** with an
`invalid_archive` code, so the UI shows an archive-specific message instead of
"file is too large to edit/upload", "conflict" or an internal error.

An archive request **stays open for the whole transfer**: the control plane
holds the connection up to its transfer budget, and the browser disables its
own client timeout, but a reverse proxy in front of the control plane must allow
a long-lived response, or the request will be cut while the agent keeps working.
The node's Traefik edge does not front the file API, so its timeouts do not
apply to it.

## Recycle bin and restore

Deleting an entry moves it into the root's recycle bin (`.gotham-trash`) instead
of erasing it. The bin is hidden from listings, search and folder sizes and
cannot be addressed by ordinary file operations. A bin with more than 5,000
entries lists the newest ones and reports `truncated=true`; the oldest are
omitted, never silently.

**Restore** returns an entry to its recorded original path, or to a destination
the caller supplies. Missing parent directories are recreated, using the mode
recorded when the entry was trashed and `0755` for a parent it never saw; a
parent that exists as a file or special file still fails instead of being
replaced. An existing destination is a `409` unless overwrite is set. **Delete
permanently** removes one entry and **Empty bin** every entry in that root's
bin; neither can be undone.

### Retention

The agent expires each writable root's bin on its own, at startup and then
hourly. Both limits are per root, and zero disables a limit:

| Variable | Default | Effect |
|---|---|---|
| `GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS` | `30` | Purges entries deleted more than this many days ago. `0` keeps every entry, which was the behaviour before this setting. Values above 36500 (100 years) are capped so the cutoff cannot overflow. |
| `GOTHAM_AGENT_FILE_TRASH_MAX_BYTES` | `0` | Caps the bytes one root's bin may hold. Once over it, the oldest entries are purged until the bin fits. `0` is unbounded. A size ceiling makes the sweep walk every trashed tree to size it, which costs more on a large bin. |

**Upgrade warning.** Before this release a bin never expired. The 30-day
default applies to entries that are already in a bin, so the first start after
upgrading permanently purges anything deleted more than 30 days ago. Set
`GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS=0` before upgrading to keep those entries.

The size pass always leaves an entry deleted within the last hour, so a large
delete stays restorable for a short while even when it alone exceeds the
ceiling; it is purged on a later sweep.

The sweep only removes valid item ids below `.gotham-trash`, and logs every
purge with the root, id, original path, bytes and reason (`age` or `size`). It
never touches anything else in the root, including the target of a trashed
symlink. It locks one item at a time, so a long purge does not block the rest of
the bin, but removing one very large tree (for example a subtree that was copied
out of a cross-device bind mount) holds that item's path lock until the walk
finishes and is retried on the next sweep if it fails part-way.

### Rollout order

Upgrade the control plane (or enable the file manager in the console) **before**
the node agent:

- A new agent behind an old control plane recycles deletes, but the old control
  plane has no bin route, so the entries are invisible until it is upgraded.
  Retention still expires them.
- An old agent behind a new control plane answers **501** for the bin routes,
  which the caller reports as an outdated node agent.

## Control-plane side

The `/api/v1/servers/{id}/files` routes are mounted by default; set
`FEATURE_FILES=false` to switch them off (they then answer **503**). The whole
surface is limited to the owner account (an owner API token needs the `admin`
scope): every file route except the ticket-gated download sits behind that gate,
so a member token, or a read- or deploy-scoped token, gets **404** or **403**,
because the API exposes the node's filesystem.

Inside the gate only the owner may use the file API, including reads
and every mutation. File bodies can hold secrets (a `0600` compose `.env`). The
ticket-gated `GET /files/download` sits outside the auth group, so the
single-use ticket is its credential.

| Variable | Default | Purpose |
|---|---|---|
| `FEATURE_FILES` | on | Set `false` to disable the file API (503 while off). |
| `FILES_MAX_UPLOAD_BYTES` | `1073741824` (1 GiB) | Ceiling for one write/upload body (413 above it); should match the agent's `GOTHAM_AGENT_FILE_MAX_UPLOAD`. |
| `FILES_SPOOL_DIR` | `<tmpdir>/gotham-spool` (0700) | Directory staging a streamed upload body before it reaches the agent. |
| `FILES_SPOOL_CONCURRENCY` | `4` | Max upload bodies staged at once; beyond it an upload answers 429. |

A streamed body is staged on the control plane (the agent's write header must
declare an exact size, which a multipart part does not carry). The spool is
swept at startup and hourly, and its free space is checked against the ceiling
before any write. The file API is served by the control plane directly, so it
does **not** traverse the node's Traefik.

`FILES_SPOOL_CONCURRENCY` is the only staging limit; a further upload while
every slot is held answers 429.

## Permissions (chmod)

`POST /files/chmod` (owner only; an API token needs the admin scope)
changes one entry's permission bits. The body is `{root, path, mode}` and
**`mode` is required**: an omitted field is refused **400** rather than silently
applied as `000`. Only the low `0o777` bits are accepted: a request carrying
setuid (`0o4000`), setgid (`0o2000`), sticky (`0o1000`) or any higher bit is
refused **400** before the agent is dialed.

A symbolic link is refused **400** (it never follows the link to change its
target), a special file (FIFO/socket/device) **409**, and a read-only root
**403**. The agent also refuses a directory that would lose its owner execute
bit (it must stay traversable) and a hard-linked regular file in a confined root
(its inode may be reachable outside the root); the whole-filesystem root has no
"outside", so the hard-link check is moot there.

Existing setuid/setgid/sticky bits are **preserved, not editable**: only the
plain permission bits change and a special bit is never created. An old agent
that predates the RPC answers **501**. chmod never changes an entry's type, so
the application-directory guard (rename/delete only) does not apply; recursion
is not supported.

The confinement opens the root and every parent directory with `O_PATH` (needs
search permission, not read), `openat`s the entry with `O_NOFOLLOW`, `fstat`s
the descriptor to classify the type, and chmods that same inode — via
`fchmodat2(AT_EMPTY_PATH)` where the kernel supports it (Linux 6.6+) and the
`/proc/self/fd` magic link otherwise. Nothing outside the root can change, a
swapped-in link cannot be followed, and an entry the agent just made unreadable
(`000` or `0o111`) can still be recovered through the UI, including by a
non-root agent.

## Permissions model

The agent runs as the unprivileged `gotham-agent` user with no
`CAP_DAC_OVERRIDE`, so **who owns a file decides what the file manager can do**:

- The `volumes` root (default `/var/lib/gotham/volumes`) is the writable root,
  but Docker creates each application bind directory as `root:root 0755`. The
  agent can list it and read world-readable files, but **cannot create, rename,
  delete or edit** anything a container wrote; a `0600` file is unreadable. This
  is the deliberate read-mostly UX with per-entry "permission denied" states.
- The `compose` root and `GOTHAM_AGENT_FILE_ROOTS` are **read-only** in the
  service even though the filesystem may allow writes (a compose edit would be
  overwritten by the next `ComposeUp`).

For containers to be editable, provisioning must make `<root>/<appID>`
agent-writable, by **either** the control plane chowning it to `gotham-agent`,
**or** a shared group plus a setgid (`2775`) application directory (the agent
must also be in that group). This is an **owner decision** pending before either
flag is flipped on for such nodes; setgid alone is not enough until the agent
joins the group.
