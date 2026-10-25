---
title: Terminal and files
---

# Terminal and files

Two host-level tools sit on top of the node registry: a **host shell** that
opens a PTY on the node, and a **file manager** that browses the node through
the agent's file service. Both are owner-only, like the application container
terminal — see [Applications](/using/applications#detail-tabs).

## Host shell

A host shell runs as the agent's service account with that account's
permissions, so it is effectively full host administration. It is available
from:

- the **Terminal** page (`/terminal`), which keeps several sessions as page tabs
  and never starts a shell on load;
- the **Terminal** tab on a node's detail page, which starts a session for that
  node;
- the floating terminal window, opened from the terminal rail or a page tab.

The console only starts a session on a **Ready** node. The target picker behind
the `+` tab lists what the signed-in owner may open: host shells for ready
nodes, and running containers of any application. The lists are bounded (50
nodes, 50 applications, 50 containers per application); a source that fails is
reported as a bounded error instead of emptying the picker, and a truncated list
says so.

### Session rules

- The session is bound to the **creating user and node**. Another user cannot
  attach to it; an unknown session and someone else's session look identical.
- A created session must be attached within **60 seconds**, or it expires
  without a PTY.
- A session is single-use. Reconnecting always means starting a new shell; a
  second attach to the same session is refused.
- The owner may hold **4** sessions; the control plane holds **64** overall.
- An attached shell lives at most **8 hours**.
- Authorization is re-checked every **30 seconds**. Logging out, revoking the
  session, changing the account or losing the owner role closes the shell
  within that window. A temporary lookup failure keeps the shell and retries;
  repeated failures close it.
- Closing a tab ends the shell and its running process.

The node runs `$SHELL` when it is bash or zsh, then prefers bash, then zsh, then
`/bin/sh`. `GOTHAM_AGENT_TERMINAL_SHELL` pins an absolute shell for a node.

## File manager

Open **Files** (`/files`) and choose a node, or use the **Files** tab on a node.
With a single managed node the chooser forwards straight to it. The node
switcher at the bottom of the browser lists every managed node; only **Ready**
nodes are selectable.

The file manager is owner-only at both ends: the console route is guarded and
every API call needs the owner account.

### Roots

Every path is relative to a root the node exposes; absolute paths are rejected.
The browser labels each root **writable** or **read-only** and shows its free
space. Roots come from the agent's configuration:

| Root | Writable | When |
|---|---|---|
| `system` (`/`) | yes | Whole-filesystem mode, the agent default. The control-plane installer enables the file manager on `localhost` by default; an added node opts in with the wizard checkbox, and either way a yes runs the agent as root. |
| `volumes` | yes | Confined mode (`GOTHAM_AGENT_FILE_FULL_ACCESS=false`): the managed-volumes directory holding application bind mounts. |
| `compose` | no | Confined mode: the compose service tree. Edits would be overwritten by the next compose up. |
| Operator roots from `GOTHAM_AGENT_FILE_ROOTS` | no | Confined mode: extra colon-separated directories. |

In whole-filesystem mode an application's top-level directory in the managed
volumes root is still protected from rename and delete while a container mounts
it. The deeper explanation of modes, ownership and permissions is in
[Remote file service](/guide/file-service).

### Browse and search

The browser offers list and grid views, sorting, a per-folder name filter, a
breadcrumb, tabs that can each sit on a different root, and folder size
calculation. A listing returns up to 5,000 entries per page; a larger folder
shows **Load more** until it is exhausted. Free space is reported for the
selected root.

Search matches **names only**, case-insensitively, under the current folder,
optionally including subdirectories. A bounded search reports that it is
showing a first set of matches instead of claiming the list is complete.

Deep links use `?root=<name>&path=<relative/path>`. They are validated against
the node's live root list before the browser opens them, and the application
**Storage** tab uses one to open a volume's folder here. An absolute path, a
`..` segment or an unknown root is rejected and the browser stays on its current
folder.

### Upload

**Upload** accepts files, not folders, and sends them one at a time. The ceiling
is **1 GiB per file**, enforced on both ends: `FILES_MAX_UPLOAD_BYTES` on the
control plane (default 1 GiB) and `GOTHAM_AGENT_FILE_MAX_UPLOAD` on the node
(default 1 GiB). Anything above the control-plane ceiling answers `413` before
the agent is dialed.

Each file reports its own result, so one failure does not abort the batch. An
existing name is a `409` and the dialog offers **Replace** or **Skip**; nothing
is overwritten without a choice. A short or aborted upload is never committed.
Uploads sharing a folder with existing entries follow the same conflict rule.
When the node's transfer budget is full (8 concurrent transfers by default) or
the control-plane staging area is full, the request answers `429`; when the
target filesystem is low on space it answers `507`.

### Create, rename, move and copy

**New** creates a file or folder; **Rename** changes one name; **Copy** and
**Cut** fill a clipboard that **Paste** applies in the current folder. All
transfers stay inside one root — a clipboard taken in another root is refused
with a message, not silently converted. A name may not be empty, `.`, `..`,
contain `/`, `\` or a NUL byte, or exceed 255 bytes; an existing name is a
`409`. A folder cannot be moved or copied into itself, and pasting into the
folder the entries already live in is refused.

### Permissions (chmod)

**Permissions…** edits an entry's mode, either as **octal** (for example `644`)
or as read/write/execute checkboxes for owner, group and other.

- Only the low `0o777` bits are editable. setuid, setgid and sticky are
  preserved but can never be set here, and a request carrying them is refused.
- A directory must keep owner execute so it stays navigable.
- A symbolic link is refused — the link is never followed to change its target.
- A special file (socket, FIFO, device) and a read-only root are refused.
- chmod is not recursive and never changes the entry type.

### View and edit

Text files open in an editor with tabs, search and replace, go-to-line, wrap,
drafts kept in the browser, and syntax highlighting. Images open in a viewer
with zoom and pan; other binaries can be downloaded but not edited.

- The editor loads up to **2 MiB** in full. A larger file opens as a read-only
  tail (the last 1,000 lines within 2 MiB) and cannot be saved.
- A binary or invalid UTF-8 body answers `415`; a save is refused rather than
  corrupting it.
- Saving checks the file's etag. If the file changed on the node after it was
  opened, the save is refused with a conflict, and the editor offers to reload
  the server version, overwrite it, or save the buffer as a copy.
- The save is written in UTF-8, ASCII, GBK, GB2312 or Big5; a character the
  target encoding cannot represent is refused.
- Every successful save stores the replaced body in the control plane's file
  history: up to **100** versions and **32 MiB** of bodies per file. History can
  be diffed, restored or deleted, and restoring saves the current body first.

### Compress and extract

**Compress…** builds a `zip`, `tar` or `tar.gz` on the node; **Extract here…**
unpacks one. Both run in the agent with Go's archive packages, never a shell.

- Compress skips symbolic links and special files.
- Extract refuses symbolic-link, hard-link, device and FIFO entries, strips
  special mode bits, and never writes outside the destination root.
- An existing file is kept unless **Replace existing files** is chosen; an
  existing directory is reused. Extract is not transactional: a failure part
  way leaves the entries already unpacked in place.
- Limits: 1 GiB archive input, 4 GiB total uncompressed bytes, 100,000 entries,
  and a fixed 200:1 compression ratio. Over-limit archives answer `413`;
  a refused member answers `409`; a corrupt or truncated archive answers `400`.

### Trash and restore

Deleting an entry does not erase it. The agent moves it into the root's recycle
bin (`.gotham-trash`), which is hidden from listings, search and folder sizes and
cannot be addressed by ordinary file operations. **Recycle bin** lists the
deleted entries newest first, with name, deletion time, size and type.

- **Restore** returns an entry to its recorded original path, or to a
  destination you choose. Missing parent directories are recreated, using the
  mode recorded when the entry was trashed. An existing name is a `409` unless
  overwrite is selected.
- **Delete permanently** removes one entry; **Empty bin** removes every entry in
  that root's bin. Neither can be undone.
- Entries stay in the bin until they are restored, removed, or expired by the
  agent's retention sweep: items older than
  `GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS` (default 30 days; `0` keeps them), and,
  when `GOTHAM_AGENT_FILE_TRASH_MAX_BYTES` is set, the oldest entries above that
  ceiling. A bin with more than 5,000 entries shows the newest ones and says the
  oldest are omitted. See [Remote file service](/guide/file-service#recycle-bin-and-restore).

### Download

**Download** mints a single-use, **60-second** ticket through the authenticated
API, then hands the ticket URL to the browser as a native download. The download
route is unauthenticated by design — a native browser download cannot send the
bearer token — so the ticket is the credential, is bound to the server, root and
path it was issued for, and is consumed on first use. The response is
`no-store`; the ticket rides the query string and is never written to the access
log. An expired or reused ticket is a `404`.

### Open terminal here

For a folder, **Open terminal here** opens a host shell with that root-relative
directory as its working directory. The shell is a floating terminal window, not
part of the file browser, and it is subject to the same owner rule
as any host shell. A symlink whose target is outside the browsable root cannot
be used as the directory. This action never opens a container shell.

## Feature flags

The console states these as product switches; the environment variables are what
the owner actually sets.

| Flag | Scope | Default | When off |
|---|---|---|---|
| `FEATURE_FILES` | Control plane | On | The file routes stay mounted but every one answers **503** with `{"message":"files are disabled","code":"disabled"}`, and the console shows its "Files are disabled" state. Host terminals are unaffected. |
| `GOTHAM_AGENT_FILES` | One node's agent | On in the agent; the control-plane installer enables it on `localhost`, the Add server wizard defaults an added node to off | The service is still registered and every file call answers **409** `file_manager_disabled`, which the console reports as “turned off on this node”. An agent that predates the file service answers **501** `agent_outdated`. |
| `FEATURE_METRICS` | Control plane | On | The metrics range route is not mounted, so the console gets **404** and hides the charts; heartbeat samples are no longer persisted. See [Nodes and containers](/operating/nodes-containers#metrics). |

`GOTHAM_AGENT_FILES` accepts `false`, `0`, `no` or `off` as off; any other value
keeps the service on. The control-plane installer enables the file manager on
its own `localhost` node by default and writes `GOTHAM_AGENT_FILES=true` plus a
service drop-in that runs the agent as root. For a node added later, the **Add
server** wizard's checkbox is off by default and the generated command carries
`--no-file-manager` or `--file-manager`. Setting the variable in
`/etc/gotham/agent.env` on the node and restarting the agent is enough to change
the answer later; the panel has no control for it.

The file manager and host shell are separate switches: `FEATURE_FILES=false`
leaves the terminal working, and `GOTHAM_AGENT_FILES=false` does not stop a host
shell.
