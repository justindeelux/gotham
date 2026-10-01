# Gotham Orchestration Checkpoint

## Stop boundary

**PHASE 9 (Self-update & Release) COMPLETE — 2026-10-01.** All packages merged and the M9
exit criteria met on `main` `7ddcd3f`:
- **BE-9.1** control-plane self-update (`390a2fa`), **BE-9.2** agent remote update
  (`ae78888`), **INFRA-9.1** release pipeline + signed installers (`9a6268d`), install
  hardening (`e99ef49`);
- **Gate G2** ran with three independent reviewers (Claude whole-chain + `code-reviewer`
  and `security-reviewer` subagents): APPROVE-WITH-CONDITIONS; every condition closed
  afterwards — e2e determinism (`#79` `8928b49`), agent-channel TLS by default + release
  environment gating + supply-chain pins (`#78` `d716734`), download budget + CLI
  ownership + CP backoff + wrapper health gate (`#77` `4220931`), docs/UI + residual
  register (`#80` `0deee90`);
- **Released and live-verified:** `v0.1.0` and `v0.1.1` are both non-draft GitHub
  releases with exactly 14 assets; the coordinator approved the `release` environment for
  `v0.1.1` under the owner's autonomy; in the clean container the real newer-release
  self-update was proven end to end (`update check` → `apply` → wrapper `result=ok`,
  version **0.1.1** with the new `gotham ca`; `rollback` + restart → **0.1.0**; re-apply →
  **0.1.1**) and `AUTO_UPDATE` was exercised (then reverted). The only M9 item still
  pending is an agent rollout against the real GitHub CDN (the mechanism is proven with
  the local release server + the two-agent systemd script).
- **Residuals** are registered in `docs/TODO.md` (Phase 9 residuals + M9 evidence),
  `deploy/README.md` (Known residuals) and the G2 review files; notable open items are the
  M4 beta-channel binding, LOW-4 agent-channel mutual TLS, key rotation/revocation,
  shared-runner/key-custody, and the `KillMode=process`/wrapper-residual family.
- **Repo state during Phase 9:** the repository was made public (with secret scanning,
  push protection, Dependabot, a `v*` tag ruleset, an approval-gated `release`
  environment); the self-hosted runner was restarted detached after it had died; the
  e2e smoke was made independent of the runner's live agent.
- **Phase gate:** per `AGENTS.md`, STOP and ask the owner before Phase 10.

## Recipe: launch an OpenCode worker (verified 2026-09-28)

Orca 1.4.215 + OpenCode 2.0.18. Follow this; do not re-derive it.

1. **Run + worktree**
   - `orca orchestration run-create --objective "<objective>" --json` → run id.
   - `orca worktree create --name <name> --repo path:/Users/ndtpro/Projects/Tools/gotham --base-branch main --no-parent --setup skip --json`
     → path `/Users/ndtpro/orca/workspaces/gotham/<name>`.
   - Rename the auto branch to convention:
     `cd <worktree> && git branch -m justindeelux/<name> feat/<slug>`.
2. **Pin the model** (project config overrides global): write `<worktree>/opencode.json` =
   `{"$schema":"https://opencode.ai/config.json","model":"opencode-go/deepseek-v4.1-flash"}`.
   Untracked; never commit; delete before PR prep.
3. **Set the variant — the TUI cycle is the ONLY method that works.** In this
   build the config `#variant` suffix and `agent.<id>.variant` are silently
   ignored (`opencode run`/session resolve to `default`). Instead, in the TUI
   send `ctrl+t` (`variant_cycle`) until the footer reads `... · max`. Cycle
   order: (default) → low → high → max → (default). The selection persists
   across new sessions/TUIs (last-used), so a fresh TUI often already shows
   `· max`.
4. **Launch the TUI terminal**
   - `orca terminal create --worktree path:<worktree> --command opencode --json` → handle.
   - `orca terminal wait --terminal <handle> --for tui-idle --timeout-ms 120000 --json`.
   - Confirm the variant: `orca terminal read --terminal <handle> --screen --json`
     and look for `Build auto · DeepSeek V4.1 Flash OpenCode Go · max`.
5. **Bind the Run to the coordinator terminal** (fencing is common):
   `orca orchestration run-use --id <run> --from <coordinator-term> --json` → `ok`
   with a generation bump. Rebind before `check --ack` if the binding flapped.
6. **Task + dispatch**
   - `orca orchestration task-create --run <run> --from <coordinator-term> --task-title "<t>" --display-name "<d>" --spec "$(cat spec.md)" --json` → `task_...`.
   - `orca orchestration worker-start --task <task> --terminal <handle> --worktree path:<worktree> --run <run> --from <coordinator-term> --json`
     → dispatch `ctx_...`, `state=ready`, `stage=input_accepted`.
   - A fresh TUI DOES start the turn directly from `worker-start`; the older
     "must manually `terminal send`" note applied only to the
     wrong-model attempt. Confirm by reading the screen: the agent is thinking
     and the footer shows `· max`.
7. **Wait for settlement**
   `orca orchestration check --wait --types "worker_done,escalation,question" --timeout-ms 1200000 --terminal <coordinator-term> --json`.
   `message` is NOT a valid `--types` value. Process the whole batch, reply to
   questions, and release or explicitly retain each settled terminal before ACK.
8. **Fix rounds**: reuse the same worker terminal (same session, variant max)
   with a new Task + `worker-start --task ... --terminal <same handle>`.

Gotchas: `worker-start --model/--effort` do not apply to OpenCode ("launches
with the model from their own config"); verify the effective variant from
`opencode session export <id>` (`info.model.variant` / message model = `max`);
`run-use` without `--from` may bind a closed or wrong terminal, so always pass
`--from <coordinator-term>`. A coordinator terminal binds exactly ONE run at a
time: to supervise two runs in parallel, bind the second to a spare terminal
handle (`orca terminal create --worktree path:<main> --command "tail -f /dev/null"`
→ `run-use --id <run> --from <spare>`), then give each run its own
`check --wait --run <run> --terminal <its-bound-handle>`; rebinding a run fences
any waiter on the other run.

## Handoff: read this first

Settled tasks 1-9 are closed (task 9 = FE domains follow-up, merged `7ccda27`);
**Phase 6 is complete**; the owner authorized Phase 7 and granted a **standing
autonomy directive 2026-09-29: for the whole of Phase 7 the coordinator acts
autonomously — merge, fix rounds, and cleanup without asking back.** The owner
extended the same grant to **Phase 8 (Advanced)** on 2026-09-29 ("tiếp tục phase
tiếp theo, cũng không cần hỏi lại"); `docs/process.md` already waives per-phase
STOPs in continuous mode from Phase 3 onward, so the coordinator runs Phase 8
end-to-end. Phase 7 is **complete**: **BE-7.1 compose services merged**
(`9832466`, PR #62), **BE-7.2 template engine merged** (`9fb7741`, PR #63) and
**FE-7.1 services UI + gallery merged** (`a7d3340`, PR #64), all cleaned up.
Phase 8 packages (`docs/plan/09-advanced.md`): BE-8.1 previews, BE-8.2 teams &
roles, BE-8.3 notifications, BE-8.4 server metrics, FE-8.1 combined UI. Run
sequentially (migrations collide). Starting with **BE-8.2 (teams)** because it is
cross-cutting. The Cloudflare test token was deleted by the owner (Phase 6
verification is done).

- Main checkout: `/Users/ndtpro/Projects/Tools/gotham`, branch `main`, at
  `a7d3340` (PR #64 merged 2026-09-29: FE-7.1; `go build ./...` exit 0).
  **Convention enforced:** task status lives ONLY in
  `docs/TODO.md`; `docs/plan/*.md` and `docs/README.md` carry no checkboxes or
  status column (roadmap/README point at TODO.md).
- **Uncommitted handoff files on main:** `docs/TODO.md` is modified and
  `docs/ORCHESTRATION.md` is untracked. Preserve both before changing checkouts,
  resetting, cleaning or moving machines. They are not in any pushed PR.
- **Merge 2026-09-28 (owner-authorized):** PR #55 marked ready and merged as
  `e0ec802` (merge commit of head `2ec800b`); main fast-forwarded cleanly,
  `go build ./...` exit 0 on the merged tree with the handoff files preserved.
  `worker-release` on `ctx_f0cf1a974961` returned `retained` /
  `processAction: none` (external operator terminal); no reclaimable terminals
  in `run_0e2895730e47`. **Cleanup done (owner-approved):** the p6-ssl worktree
  was removed via `orca worktree rm`, local branch deleted by Orca and remote
  `feat/p6-ssl` deleted; the worktree's live terminal `term_d60e7e5a` was closed
  (`--tab`). `term_38daecc0` is stale (no longer live); only the coordinator
  terminal remains. Reports/evidence stay under `/Users/ndtpro/orca/workspaces/gotham/`.
- The old `p6-traefik` worktree was removed in the owner-requested cleanup
  (merged branch `feat/p6-traefik` deleted at `ef29769`). The later `p6-ssl`
  worktree (branch `feat/p6-ssl`) was merged as `e0ec802` and removed on
  2026-09-28; no package worktree remains. Refresh actual branch/PR state before
  relying on this snapshot.
- No active Dispatch remains in run `run_858e742082eb`. The wrong-model attempt
  `ctx_428ae01c8b99` was abandoned before any turn; its idle terminal
  `term_7e207631-4fd8-4b3e-8b9c-bd69ff8ff42c` owns no work. Old IDs below are
  evidence references, not credentials or permission to resume a settled Dispatch.
- **Cleanup 2026-09-28 (owner-requested):** completed worker terminals closed
  (wrong-model idle `term_7e207631`, P5 fresh `term_943bfad8`, stale P5-smoke
  agent `term_673deaac`); merged worktrees removed (`p6-traefik`,
  `p5-backup-lows`, `p5-backup-smoke`) with the `opencode.json` model pin
  deleted; merged branches deleted (`feat/p6-traefik`, `fix/p6-l1l2`,
  `fix/p5-backup-lows`, `justindeelux/p5-backup-smoke`). Kept: coordinator
  terminal, operator-created P6 terminal `term_c9a8ffba`, possibly-user-owned
  reviewer terminal, plain shells. Reports/evidence remain under
  `/Users/ndtpro/orca/workspaces/gotham/` (workspaces dir itself kept).
- Reports and raw evidence are **outside Git**, under
  `/Users/ndtpro/orca/workspaces/gotham/`. Preserve/copy them if the handoff moves
  to another environment; do not clean the workspaces before retrieving them.

Suggested first checks: read this file and `TODO.md`, inspect both checkouts with
`git status`, confirm PR #55/#56/#57 are merged and main is at `37741c3`, and use scoped
Orca run/worker state to confirm no concurrent editor. Load the orchestration
skill's version-matched guide through the resolved CLI (`orca` 1.4.215); use the
new coordinator's actual terminal handle.

## Active task (Phase 9): INFRA-9.1 release pipeline

Launched 2026-09-30 after BE-9.2 merged (`ae78888`), under the Phase 9 autonomy
(execution effort `high`). Scope from `docs/plan/10-self-update-release.md`
(INFRA-9.1): `.goreleaser.yaml` building `gotham` + `gotham-agent` for linux
amd64/arm64 with the version and the **embedded Ed25519 public key**
(`-X …/updatecore.PublicKey=…`), a `v*`-tag `release.yml` on the self-hosted runner that
signs the manifests with `cmd/signer` from a repo secret and publishes the release
assets + checksums (failing when the keys are missing), `deploy/install.sh` verifying
the signature + digest before installing into the BE-9.1 layout, and install/update
guides. The asset names must match exactly what `updatecore`/`internal/updates` resolve.

- Run `run_32feaf40a183`; Task `task_d0fbc0b3a026`; Dispatch `ctx_0efc83c8802b`.
- Worker terminal `term_db1fb69b-1263-4696-81e3-f53ef0367371` (OpenCode, variant
  **`high`**), worktree `/Users/ndtpro/orca/workspaces/gotham/p9-release`, branch
  `feat/p9-release` from main `ae78888`; untracked `opencode.json` pin.
- Status: dispatched. Sequence: verify → draft PR → Claude review (new session in that
  worktree, kept per the policy) → the coordinator's real checks (repo secrets for a
  keypair, tag `v0.1.0` to run the release workflow, released assets, a clean
  systemd-container install + login) → merge → **gate G2** (code-reviewer +
  security-reviewer over the whole update/signature/release/install chain) → the Phase 9
  milestone report.
- **Release keypair provisioned by the coordinator (2026-09-30):** `cmd/signer keygen`
  wrote the PKCS#8 private key + PKIX public key to `~/.gotham-release-keys/`
  (`signing.key` 0600, outside the repo — the owner must back it up) and the GitHub repo
  secrets are set: `GOTHAM_UPDATE_SIGNING_KEY` (private PEM, piped via stdin, never
  printed) and `GOTHAM_UPDATE_PUBLIC_KEY` (base64 raw Ed25519 public key,
  `Yt6nz1gGQWF7Bfc9MCt/gQXbPMzhN9OygrUkOEFYdwQ=`; the ldflag target is
  `github.com/justindeelux/gotham/updatecore.PublicKey`). The worker was told to pin the
  public key in the installer (no trust-on-first-use) and to fail the workflow when
  either secret is missing. The coordinator will tag `v0.1.0` to run the real release
  and then install into a clean systemd container.
- **Round-1 verdict (Claude Code, INFRA-9.1): FIX-FIRST** — the trust chain is verified
  correct (asset names match the update code, the pinned key equals `Yt6nz…`, the ldflag
  targets `updatecore.PublicKey`, missing/mismatched secrets stop the release, the
  installers verify the signature before the hash, no private key material in the repo).
  But the planned `v0.1.0` would have failed: **H1** the draft-release lookup used
  `/releases/tags/{tag}` (404 for drafts) and a fragile `grep '"id":[0-9]*'` (the API
  puts a space after the colon), so the draft got binaries but no manifests and was
  **never published**; **H2** without `GOTHAM_VERSION` the installer derived the tag from
  the final download URL (no tag) and **always aborted** — neither path was covered by
  the dry run (it never exercises the real GitHub API or the default install path).
  Also required: **M1** a completeness check before publishing (missing assets were
  skipped silently), **M2** re-install must preserve operator env values and `umask 077`
  around secrets, **M3** the test-only key override must not work in production. Lows:
  the docs/installer admin claim contradicts `PLATFORM_ADMINS`, actions pinned by tag
  (not SHA), and a leftover discarded signing key in the workspace (deleted by the
  coordinator). **M4 tracked for the Phase 9 residuals:** beta-channel installs reject
  every stable release (pre-existing BE-9.x manifest check). **Fix round 2 dispatched**
  (Task `task_321322345525` / Dispatch `ctx_173aeea6091c`); then the coordinator tags
  `v0.1.0` (the real run will exercise H1/H2), installs on a clean systemd container,
  and Claude round 2 re-reviews.
- **Fix round 2 delivered `0d8b64a`**: the draft is located via
  `GET /releases?per_page=100` + `jq` (tag/draft match), a **14-asset completeness
  assertion** gates publishing, and `PATCH draft=false` is asserted to have taken
  effect; the installers resolve the latest tag from the `/releases/latest` redirect and
  pin every download to it (`GOTHAM_RELEASES_URL` mirror override), the dry run covers
  the default and re-install paths (8 cases); M1-M3 fixed (exact counts, `umask 077` +
  env preservation, test-only override renamed/gated); lows fixed (SHA-pinned actions,
  `--version` check, `RUNNER_TEMP` signer, corrected docs, private mktemp dir, orphan key
  removed). **Round-2 verdict (Claude Code): FIX-FIRST** — every round-1 item is verified
  fixed, but the M2 fix introduced **N1**: the global `umask 077` makes `mkdir -p`
  create `/etc/gotham` and `/usr/libexec/gotham` as 0700 root:root on a clean host, so
  the CP cannot read its JWT key (startup fails, restart loop) and self-update fails on
  both sides, while the installer still reports success; the dry run cannot see it (one
  non-root user owns everything) and `apt-get` also ran under 077. **Fix round 3
  dispatched** (Task `task_e644fe7100d4` / Dispatch `ctx_1e472136e405`): scope the umask
  to the secret/env writes, create the shared dirs 0755, add 0755/service-user
  assertions to the dry run, and an early `jq` presence check. The coordinator confirmed
  `jq 1.6` is installed on the runner.
- **Fix round 3 delivered `e340d30`** (the worker_done delivery was rejected by Orca for
  a mangled terminal handle, but the task settled `completed` and the commit is on the
  branch): the base umask is 022, `umask 077` is scoped to the secret/env writes, all
  shared directories are explicitly 0755, `apt-get` runs under a normal umask, the dry
  run asserts the 0755 dirs / readability / wrapper executable (non-vacuous: a 0700 dir
  trips it) plus a static guard for `install-agent.sh`, and an early `jq` presence check
  runs before the build. **Round-3 verdict (Claude Code): MERGE-GO** — N1 verified closed
  (the reviewer re-broke it in a copy: the dry run fails with `mode is 700, want 755`),
  small items done, no regressions.
- **INFRA-9.1 MERGED `9a6268d`** (PR #73), main ff'd, `go build ./...` ok. **Tagged
  `v0.1.0`** → the real Release workflow ran on the self-hosted runner and
  **succeeded**: the GitHub release is **non-draft** with exactly **14 assets**
  (4 binaries amd64/arm64 for CP+agent, 4 signed manifests, 4 signatures,
  `checksums.txt`, `gotham-signing-key.pub`) — H1 proven end-to-end. The clean
  systemd-container install (Ubuntu 22.04, public anonymous clone at the tag, no
  `GOTHAM_VERSION` set) is running to prove H2 + the reviewer's checklist (CP active,
  sign-up/login, the `gotham` user can read the JWT key and exec the wrapper). Residuals
  to track: the UI's one-liner install command references
  `releases/latest/download/install-agent.sh`, which is **not** a published asset
  (publish the installer scripts as assets or fix the UI string), the beta-channel
  stable-rejection (M4), and the Go 1.22 EOL build note.
- **Container install test (in flight):** `jrei/systemd-ubuntu:22.04` on the box needs
  `--privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw --tmpfs /run
  --tmpfs /tmp --tmpfs /run/lock` (host is cgroup v2; without `--cgroupns=host` the
  container exits immediately). Inside: install curl/ca-certificates/openssl/git, clone
  the public repo **at tag `v0.1.0` without credentials**, then run
  `bash deploy/install.sh` with **no `GOTHAM_VERSION`** (the H2 path) and verify the
  reviewer's checklist (CP active, dirs 0755, the `gotham` user can read the JWT key and
  exec the wrapper, HTTP on :8000, sign-up/login). Next: **Gate G2** — Claude (same
  session, whole-chain brief) + the mandated `code-reviewer` and `security-reviewer`
  subagents over the integrated Phase 9 chain (BE-9.1 + BE-9.2 + INFRA-9.1: signatures,
  distribution channel, runtime privileges, installers), then the Phase 9 milestone
  report and the phase gate for the owner.
- **Container install finding (hardening):** on a minimal Ubuntu 22.04 without `sudo`
  (the jrei image), `deploy/install.sh` runs all the way to the last step and then dies
  with `install-sudoers.sh: 51: cannot create /etc/sudoers.d/gotham-update: Directory
  nonexistent` — the script assumes `/etc/sudoers.d` exists and the prerequisites do not
  check for `sudo`/`visudo`. A standard Ubuntu 22.04 VPS has `sudo` (the jrei container
  is minimal), so the plan's target host still works, but the install should
  `mkdir -p /etc/sudoers.d` (mode 0750) and fail early with a clear message when
  `sudo`/`visudo` is missing. Recorded for the G2 fix round. The re-run with `sudo`
  installed is in flight to complete the end-to-end proof.
- **Container finding 2 (blocking, found by the re-run):** with `sudo` present, the
  second install aborted (`exit 1`, no message) right after `==> writing
  /etc/gotham/gotham.env`. The `bash -x` trace shows the M2 preservation pipeline
  `printf … | grep -v -E '^(known keys)=' | grep -v -F '# comment'` exits 1 when the
  existing env contains **only** the default keys plus the header (the final `grep -v`
  outputs nothing), and `set -e` kills the script — so re-installing on a host with no
  operator additions always fails (the dry run's re-install case evidently had
  additions). **Install-hardening work package opened:** worktree `p9-install-fix`,
  branch `feat/p9-install-fix` from main `9a6268d`, worker terminal `term_4bdf695e-64ca-4b35-9d7f-9b99628dd12d` (variant `high`), Task `task_858a2b84c8e6` / Dispatch
  `ctx_de65014a3ae6`. Scope: fix the empty-result filtering (B1) with dry-run cases
  (install twice with no additions succeeds; operator additions survive; managed DSN
  retained), harden `install-sudoers.sh` (`mkdir -p /etc/sudoers.d` 0750, clear
  early failure when `sudo`/`visudo` is missing, prerequisites + docs) (B2), and only if
  cheap publish the installer scripts as release assets (else leave a note). Then the
  coordinator re-runs the clean-container install twice, and Gate G2 starts.
- **Install-hardening delivered `eb7d2dc`** (draft PR #74, worktree `p9-install-fix`):
  B1's pipeline tolerates the empty `grep -v` result (`install.sh:314`) while preserving
  operator additions/known keys/managed DSN; B2 creates `/etc/sudoers.d` 0750, validates
  with `visudo -cf` unconditionally and fails early when `sudo`/`visudo` is missing
  (both installers + prerequisites + docs); a non-vacuous dry-run case (reverting the fix
  reproduces the failure) and B2 static guards were added; publishing the installer
  scripts as release assets was left as a note (they need their sibling files).
- **Container verification with the fixed script (coordinator, real container on the
  box):** `install.sh` ran **twice, both exit 0**; `systemctl is-active gotham` → active;
  `/etc/gotham`, `/usr/libexec/gotham`, `/var/lib/gotham`, `/var/lib/gotham-updater` all
  755; `sudo -u gotham test -x /usr/libexec/gotham/gotham-update` ok; `sudo -u gotham
  head -c1 /etc/gotham/jwt_ed25519.key` ok; the UI serves **200**; **sign-up 200** with a
  token and **login 200**. Every reviewer checklist item for the real `v0.1.0` install is
  therefore proven. Claude review of PR #74 is in flight; on MERGE-GO the coordinator
  merges, then runs **Gate G2** (Claude whole-chain + `code-reviewer` + `security-reviewer`
  subagents) and writes the Phase 9 milestone report for the owner.
- **PR #74 review: MERGE-GO** (Claude) — B1 is fixed at the root cause (`|| true` covers
  the whole pipeline; unusual inputs tested: look-alike keys kept, `=`/`|`/`$` values
  preserved, duplicate managed keys written back once, reordered/missing-header files
  handled), B2 stops early with clear messages, creates the dir 0750 and always
  validates; the new dry-run case is non-vacuous (`FAIL: re-install with no operator
  additions failed (B1 regression)` with the fix reverted); no regressions, sweep clean.
  Three non-blocking LOWs were folded into the same draft PR via **fix round 2**
  (Task `task_fb963c89c38d` / Dispatch `ctx_7bb8bf9b06a1`): L1 tolerate only the "no
  lines" status instead of blanket `|| true`, L2 run the sudo/visudo precondition only
  when the sudoers step executes (and resolve `visudo` absolutely when root), L3 validate
  a temp copy before it lands in `/etc/sudoers.d`, plus the leading-whitespace managed-key
  note and the docs' `visudo` mention. **Bonus verified in the container:** the CP's own
  `GET /api/v1/updates/check` returns `{"current":"0.1.0","available":false}` — the
  update checker works against the now-public GitHub Releases (the private-repo gap is
  closed), and `/api/v1/servers/agents` correctly requires platform-operator access.
- **Round-2 review of PR #74: FIX-FIRST** with two must-fix findings: **N1** (from
  BE-9.2 `20fa940`) `deploy/install-agent-sudoers.sh` was committed **mode 644** while
  `install-agent.sh` runs it directly, so the documented node install from a checkout
  dies with `bad interpreter: Permission denied` after the user/binary/wrapper exist —
  the coordinator's container check only ran the CP installer, which is why it was
  missed; **N2** L1 was only half fixed (only the last grep's status was inspected, so a
  first-grep failure read as "no operator settings" and silently dropped lines);
  **I3** a root-only temp `gotham.env` (with the secret key) could survive an abort.
  L2/L3/notes were verified correct. **Fix round 3 delivered `f16da35`**: the mode is
  restored to 100755 in git, both sudoers helpers are invoked via `sh`, a static test
  asserts the directly-invoked/operator-run scripts are executable (the four installers
  committed 100755), the preservation filter is a single grep, and an EXIT/INT/TERM trap
  removes the temp secret; each fix has a non-vacuous test. **Round-3 review (Claude) and
  the container re-check are in flight**; on MERGE-GO: merge PR #74, then run Gate G2 and
  write the Phase 9 milestone report.
- **PR #74 round-3 review: FIX-FIRST** with **F1 (high, introduced by the I3 fix)**: the
  new `trap 'rm -f "${env_tmp}"' EXIT INT TERM` removes the temp file on INT/TERM but
  does not exit, so the install continues, the next append recreates the temp with only
  operator lines and `mv` installs that as `gotham.env` — the secret key, DSN, Redis, CA
  and JWT entries are lost while the install exits 0 (reviewer reproduced with TERM under
  bash and dash). N1 and N2 are verified fixed (all `deploy/*.sh` committed 100755, the
  helpers invoked via `sh`, the static test non-vacuous; the single-grep filter cannot
  mask an upstream failure; the container re-check passed again: two installs exit 0,
  service active, mode 755, UI 200). **I4** informational: the top-level `WORK_DIR` trap
  has the same no-exit shape (predates the PR). **Fix round 4 dispatched** (Task
  `task_4e366fbab659` / Dispatch `ctx_63b143f37f65`): cleanup on EXIT only + `trap 'exit 1'
  INT TERM`, a TERM regression test under bash and dash, and the same split for the
  WORK_DIR traps. **Flake check:** the coordinator ran the FIFO test **50× on the box with
  0 failures**, so the reviewer's single `TestWrapperHardensPendingRead/fifo` failure
  looks like a rare load-dependent flake, not a reproducible race — tracked, no code
  change planned (G2 will see it noted).
- **Fix round 4 delivered `a86a27a`**: the env-subshell trap is split into cleanup on
  EXIT plus `trap 'exit 1' INT TERM` (`install.sh:311-312`), the same split is applied to
  the top-level `WORK_DIR` traps in both installers (`install.sh:229-230`,
  `install-agent.sh:155-156`), a static guard catches cleanup-only traps, and the new F1
  test TERMs the env subshell (grep shim targeting the grandparent pid) asserting a
  nonzero exit, a byte-identical `gotham.env` and no leftover temp — it fails on the
  reverted trap under bash and dash. Deferred: a runtime test for the I4 window (static
  coverage only), the duplicate-managed-key precedence quirk, and the release-asset
  publishing note. The final container re-check and Claude round 4 are in flight; on
  MERGE-GO: merge PR #74, then **Gate G2** (Claude whole-chain + `code-reviewer` +
  `security-reviewer` over the integrated Phase 9 chain) and the Phase 9 milestone
  report for the owner.
- **GATE G2 ran (2026-10-01) — all three reviewers: APPROVE-WITH-CONDITIONS.** Reports:
  `g2-review-code.md` (code-reviewer subagent), `g2-review-security.md`
  (security-reviewer subagent), and Claude's whole-chain verdict (in flight when this
  was written). The chain's core is verified sound (single `updatecore` engine,
  signature/digest trust chain, atomic swap + rollback, privilege separation, installer
  verification; the security reviewer independently re-verified the live `v0.1.0`
  artifacts: all four manifests verify against the pinned key, GitHub's asset digests
  equal the signed manifest digests, checksums match, the binaries embed the pinned key).
  **Conditions (consolidated):**
  * **HIGH (security)** — the signing key + `contents: write` live on the persistent
    self-hosted runner that PR CI reaches; a hijacked run on the next tag could sign a
    malicious release or exfiltrate the key. Fix: environment-gated release (+ runner
    isolation when available). *Coordinator already done:* created the GitHub **`release`
    environment with a required reviewer (owner)**, moved both update secrets into it.
  * **HIGH (code)** — the CP↔agent gRPC channel is **plaintext on a fresh install** (no
    CA is ever created; the docs/plan claim otherwise); update integrity still holds via
    the embedded-key signatures, but the mTLS requirement is unmet.
  * **MEDIUM** — artifact downloads are hard-capped at 10 s (large assets fail on slow
    links); `sudo gotham update apply` leaves the binary root-owned (the next service
    apply EPERMs); the UI's install one-liner 404s (asset not published and needs
    siblings).
  * **LOW/INFO** — `rollback` prints success without restarting; CP `AUTO_UPDATE` lacks
    the agent's rollback backoff; M4 beta-channel binding; mutable action pins + the
    unverified sqlc download on the runner; docs advertise a UI that does not exist; M4
    not recorded in repo docs; a quote-injection in the installer's DSN interpolation;
    M9 criteria 1-3 partially met (no real *newer* release applied via `gotham update`
    or a rollout; `AUTO_UPDATE` unexercised); wire `test-release-install.sh` into CI.
  * **Other hardening the coordinator applied (repo settings):** secret scanning + push
    protection enabled, Dependabot alerts + security updates enabled, a ruleset
    protecting `v*` tags (deletion/non-fast-forward/update) — branch protection for
    `main` left as an owner decision.
  * **Two G2-fix work packages dispatched (parallel, effort high, 2026-10-01):**
    **A** `feat/p9-g2-channel` (Task `task_d9aece17aa85` / Dispatch `ctx_3b0d7d51ec1f`,
    worker `term_227a403c`) — CA creation + agent fail-closed TLS, `environment: release`
    in the workflow, SHA-pinned actions + sqlc checksum, DSN quoting; **B**
    `feat/p9-g2-update` (Task `task_a4730e03d03c` / Dispatch `ctx_d677970d3f5c`, worker
    `term_16ef0f72`) — separate long artifact-download bound, CLI ownership guard,
    rollback message, CP rollback backoff, CI wiring of the installer tests. A docs/UI
    work package (the UI one-liner, docs corrections, M4/M9 tracking) follows both.
- **Merged 2026-10-01:** **#79** e2e determinism fix (`8928b49`) — every e2e node seed now
  uses an unused loopback address (`[::1]:1`), so the CP's proxy push gets a fast
  connection-refused instead of reaching the shared runner's live agent (the diagnosis was
  confirmed by reverting it: `Unimplemented agent.v1.ProxyService` + pending record +
  30 s timeout; with the fix the full smoke passes on the runner and on the box).
  **#78** G2 channel/release/supply-chain work (`d716734`) — CA provisioning + TLS by
  default, the agent installer fails closed without the CA, remote-agent SANs, the agent
  offer bound to its family/channel (C2), `release.yml` gated on the `release` environment,
  SHA-pinned actions, the sqlc checksum, the DSN quoting and the installer CI job. Both
  merged with all checks green (including UI E2E on the runner). **#77** is rebasing onto
  it now (Task `task_f9536fa12f05`): keep #78's pinned ci.yml, merge the `ca.go` hosts
  work with the N1 trim, keep both docs/settings sets. Then: docs/UI pack, tag `v0.1.1`,
  the real update test, the Phase 9 milestone and the phase gate.
- **#77 merged `4220931`** after a clean rebase onto #78 (only `docs/install.md`
  conflicted; `ci.yml` keeps #78's SHA pins and adds the installer job, `ca.go` carries
  both the SAN hosts work and the `uniqueStrings` trim). CI on the rebased head was fully
  green (installer job 7m20s, E2E success) and `main` now builds clean.
- **Docs/UI pack (draft PR #80, `8f00215`)**: the wizard and ServersPage now show the
  working checkout-based agent install (the old `curl | sh` one-liners 404'd), the
  fabricated 'mutual mTLS' copy is corrected to server-authenticated TLS,
  `docs/install.md` says to run `gotham update` as the service user and points at the
  operator update-all API instead of a non-existent UI button, and the M4/LOW-4/key
  rotation/shared-runner/I5/`GOTHAM_UPDATE_CURRENT` residuals plus the proven-vs-pending
  M9 evidence are tracked in `deploy/README.md`, `docs/TODO.md` and `docs/plan/10`.
  `webdist` rebuilt with zero drift. Claude review in flight; then tag `v0.1.1` and run
  the real newer-release update (the last M9 evidence).
- **G2-fix A delivered (draft PR #78, `feat/p9-g2-channel`)**: `gotham ca init` + installer
  provisioning so the CP's gRPC gateway runs TLS on a fresh install; `install-agent.sh`
  requires the CA (`--ca`/`GOTHAM_AGENT_CA_FILE`, fail-closed, `--insecure` dev override);
  `release.yml` gated on the **`release` environment** (approval + environment secrets,
  `contents: write` only there); every CI action SHA-pinned; the sqlc tarball
  checksum-verified; the DSN passed positionally; a CI job runs the installer tests +
  `sh -n` + actionlint. **C1-bis verified by the coordinator in the diff:**
  `agent/tls.go:82` now sets `tls.RequireAndVerifyClientCert` when a CA is configured and
  the CP dials agents with its client cert (`internal/servers/docker_client.go:171`), so
  the node-control channel is mutual; the remaining gap is the CP gateway's
  agent→CP direction (`VerifyClientCertIfGiven`, LOW-4: registration has no bootstrap
  credential) and is documented. **C2** (offer family/channel binding) was assigned as an
  addition to A — the review round checks whether it landed or is explicitly deferred.
  Claude's review of PR #78 is in flight; worker B is still running.
- **INFRA-9.1 round 0 delivered `b6c4b37`** (draft PR #73): `.goreleaser.yaml` (raw
  binaries named exactly as the update checkers resolve them, version + Ed25519 key via
  `-ldflags`, checksums, and the public key published as an asset), `release.yml`
  (fails closed when either secret is missing, signs the per-arch manifests with
  `cmd/signer` over the built bytes, publishes the draft with all assets, and
  **verifies the secret keypair matches the committed pinned public key**),
  `deploy/release-verify.sh` (shared fail-closed verification), a signed
  `deploy/install.sh`, a signature-verified `deploy/install-agent.sh`,
  `deploy/test-release-install.sh` (the local dry run), `docs/install.md`, and a
  contract test pinning the asset names and the trust anchor. Locally: `goreleaser
  check`, a snapshot build with the key embedded in all four binaries, the full
  sign→serve→verify→install dry run with tampered cases failing closed, actionlint and
  the Go gates all green. **Key mismatch found and fixed:** the worker had pinned a
  locally generated key while the repo secrets hold the coordinator's provisioned
  keypair, which the workflow's own consistency check would have rejected; **fix round 1
  dispatched** (Task `task_b2d08bcdded8` / Dispatch `ctx_9415643bb947`) to pin the
  provisioned public key (`Yt6nz…`) everywhere and keep the dry run on a test keypair
  via the documented override. No private key material was ever in the worktree or the
  report. Then: Claude review (new session in `p9-release`), merge, tag `v0.1.0` for the
  real release, the clean-container install, and gate G2.
- **Private-repo note (owner decision / G2 input):** `justindeelux/gotham` is **private**.
  The release workflow works (it uses the CI token), and `install.sh` supports a mirror
  (`GOTHAM_BASE_URL` + `GOTHAM_VERSION`) — which is how the coordinator will test the
  install with the real signed assets. But the runtime update checkers
  (`internal/updates/checker.go` for the CP, the agent offer path) query the GitHub
  Releases API **without a token**, so on a private repo they would 404 (fail closed,
  updates disabled). Options: make releases public (public repo), or add a
  `GOTHAM_UPDATE_TOKEN` to the checker/agent as a small follow-up. Recorded for the
  Phase 9 milestone/G2; not a blocker for the container verification (mirror-based).
- **Repo made PUBLIC by the coordinator (owner asked, 2026-09-30).** Pre-flight: full
  history scanned for secrets (0 real PEM private keys — the 65 `BEGIN OPENSSH PRIVATE
  KEY` hits are the UI form placeholder, the bundled JS and test fixtures; 0
  GitHub/AWS/Slack/OpenAI/Google tokens; 0 JWTs; no `.env`/logs/dumps/binaries tracked;
  879 tracked files, 55 MB `.git`); the untracked `docs/ORCHESTRATION.md` and the
  worker reports were never committed. `gh repo edit --visibility public` succeeded
  (admin + `repo` scope). **Runner-safety hardening applied immediately:** fork-PR
  approval policy = `all_external_contributors` (no fork workflow runs without
  approval) and default workflow permissions = `read` (no PR approvals). Verified
  anonymously: repo `private=false`, releases API 200, source download 302. This
  resolves the private-repo token gap (anonymous Releases API now works). **Residual
  note for the owner:** a self-hosted runner on a public repo is inherently riskier
  than one on a private repo — the approval gate mitigates fork PRs, but if/when
  GitHub-hosted Actions billing is restored, moving CI back to GitHub-hosted runners
  would remove the class. The repo still has **no LICENSE file** (recommend adding one
  before announcing the project publicly).
- **Fix round 1 delivered `526eb0b`**: `deploy/gotham-signing-key.pub` and both
  installers now pin the provisioned key (`Yt6nz…`), matching the repo secrets; the dry
  run keeps an ephemeral test keypair via the documented override and now also asserts a
  pinned-key mismatch fails closed. Claude review round 1 (new session in `p9-release`)
  is in flight.

## Settled task (Phase 9): BE-9.2 agent remote update (merged `ae78888`)

**Merged 2026-09-30**: PR #72 (`feat/p9-agent-update`) merged as `ae78888`, CI **9/9
SUCCESS**, local main fast-forwarded, `go build ./...` exit 0. Five independent review
rounds (Claude Code + one security subagent pass) plus the **real two-agent systemd
proof** on the Linux box, which found and fixed three product issues the reviews had
missed: systemd's start rate limit blocking the rollback restart (`reset-failed`), the
agent re-applying a release that had just rolled back (startup backoff seeding +
`gotham-agent update reset`), and the reset marker's symlink/FIFO/chmod-by-path races
(handle-based, no-follow writes). The shared `updatecore/` package carries the BE-9.1
engine (agent/ imports no internal/), the CP resolves the agent release family and
serves signed offers behind an operator-gated update-all API, and the agent verifies,
swaps, restarts and reports on reconnect. Residuals: the agent channel is
server-authenticated TLS (no agent client cert yet), the health check is liveness-only,
and INFRA-9.1 must embed a non-empty public key. Per the keep-workers policy the
`p9-agent-update` worktree/branch and the Claude review session were **retained**.


Launched 2026-09-30 after BE-9.1 merged (`390a2fa`), under the Phase 9 autonomy
(execution effort `high`). Scope from `docs/plan/10-self-update-release.md` (BE-9.2): the
CP keeps an agent version map and pushes `RequestUpdate` (URL + manifest + signature) →
the agent downloads, verifies the Ed25519-signed manifest, swaps atomically with
rollback, restarts and reports the new version on reconnect; `UpdateService` on both
sides; an operator-gated "update all agents" API; tests include a fake release pushed to
an agent that reports the new version after reconnect. A neutral shared package carries
the BE-9.1 verification core so `agent/` never imports `internal/`.

- Run `run_50f75c659936`; Task `task_816adca216c4`; Dispatch `ctx_1b05f70aac82`.
- Worker terminal `term_07b02ace-2de0-44ba-9bb6-cd6394df48ec` (OpenCode, variant
  **`high`**), worktree `/Users/ndtpro/orca/workspaces/gotham/p9-agent-update`, branch
  `feat/p9-agent-update` from main `390a2fa`; untracked `opencode.json` pin.
- Status: **fix round 1 in flight** (Task `task_60fbe1d467dd` / Dispatch `ctx_ea0f0a682ff8`).
  Round 0 delivered `20fa940` (draft PR #72, CI 9/9): the transport-agnostic engine moved
  to a new top-level `updatecore/` package (so `agent/` imports no `internal/`), the
  proto gained the signed-offer fields, the CP implements `RequestUpdate` plus an
  operator-gated agent version map/update-all API, and the agent polls, verifies,
  swaps, restarts and reports its version. **Two independent reviews (Claude + a
  security subagent) both said FIX-FIRST** with the same core findings: **H1**
  "update all agents" is a no-op once the CP itself is current (it compares the agent
  against the CP's own version instead of the target agent release); **H2** the agent
  never calls `ResumeStaged`, so a crash during the health window leaves the unproven
  binary running with every later update rejected (`ErrUpdatePending`); **M1** the
  agent records the new version when the wrapper *starts*, so a failed update reports
  the new version and is never re-offered; **M2** `RequestUpdate` records versions for
  callers with no client certificate (unbounded map; a certless client wrote 1000
  entries) and hits the GitHub API on every poll (20 requests ⇒ 20 calls, exhausting
  the anonymous rate limit shared with the CP's own checks); **M3** the agent accepts a
  validly signed **older** release (no anti-downgrade). Lows: a stale ldflag comment
  path in `updatecore/sign.go` (would ship a keyless binary), the agent default binary
  path, one timeout covering RPC+download, `install-agent.sh` sibling coupling, and
  exact-version rollout matching. Fix round 1 covers all of them; then Claude round 2
  (same session dir), the real two-agent systemd check on the box, and merge.
- **Fix round 1 delivered `92b8026`** (CI green): `update-all` now targets the newest
  **agent** release and rolls out to agents whose version differs; the agent calls
  `ResumeStaged` at startup (non-blocking lock, relaunch-once); it adopts a new version
  only when the durable status is `ok`; `RequestUpdate` no longer writes the version map
  (heartbeats only, with cap/TTL/length bounds) and the resolved release is cached with
  a TTL + stale-verified fallback (N offers ⇒ 1 upstream fetch); the agent refuses
  equal/older offers; L1-L5 fixed (ldflag comment path, agent default binary path, split
  RPC/download timeouts, installer early-fail, differing-version rollout matching).
  Regression tests added for each. Claude round 2 is in flight; next the real two-agent
  systemd check: the coordinator will have a runnable `deploy/verify-agent-update.sh`
  (scratch CP + scratch DB/ports, two agent versions, a locally signed release served
  over loopback http, `update-all`, convergence) so the box proof mirrors the BE-9.1
  one.
- **Fix round 2 delivered `38624ed`** (CI 9/9): N1 negative-cached release lookups with
  single-flight (a failing upstream is hit once per backoff; 20 offers ⇒ 1 call), N2 a
  5-minute freshness window on the durable `ok` status, N3 README wording fixed to
  server-authenticated TLS, N4 parser-normalized pending versions, N5 per-version
  exponential backoff after a failed update. Plus **`deploy/verify-agent-update.sh`**,
  the root-only self-cleaning merge gate: builds the CP + two signed agent versions with
  the release key embedded via `-ldflags`, signs/serves a loopback release, runs a
  scratch CP + two scratch systemd agents with the shared wrapper/sudoers, and proves
  C1 (update-all with the CP already current starts a rollout), C2 (both agents restart
  through the wrapper and record `ok`), C3 (both heartbeats converge; planted install
  byte-identical), C4 (the CP resolves the agent release family), NEG1 (tampered asset
  stays on the old version) and NEG2 (a validly signed broken release rolls back). The
  coordinator's box run is in flight; then Claude round 3 (delta) and merge.
- **Box-proof iterations:** the first run failed at operator registration — a real
  script bug, diagnosed exactly: the CP health wait at `verify-agent-update.sh:272` used
  `code=$(curl … -w '%{http_code}' … || echo 000)`, so a failed probe produced
  `"000\n000" != "000"` and the loop broke on the **first** attempt; the script then
  registered before the scratch CP had bound (`curl: (7) Connection refused` while the
  CP's own log showed `http server listening` ~200 ms later). **Fix round 3 delivered
  `df0e278`** (script only): wait on curl's exit status, retry registration with
  backoff and print status/body/CP-log on failure, `VERIFY_KEEP=1` for debugging, and
  an audit of every other wait. The box was cleaned (processes, scratch DB/dir) and the
  script is being re-run on the box now.
- **Box proof hits: after fix 3 the run passed C1, C2, C3, C4 and NEG1 but failed
  NEG2**, and the scratch journal/state gave a **real product gap**: NEG2 signs a valid
  v4 over a binary that exits 1 — the agents applied it, the wrapper restarted the
  units, the broken binary crash-looped (`Restart=always`, `RestartSec=2`), systemd hit
  its start rate limit (*"restart counter is at 9"* → *"Start request repeated too
  quickly"*, unit `failed`), the wrapper correctly failed health and restored the
  healthy v2, but its `systemctl restart` was **refused by the rate-limited unit**, so
  it recorded `result=rollback_failed` ("previous binary is also unhealthy") and the
  agent stayed DOWN (both agents). The real units (`gotham.service`,
  `gotham-agent.service`) use `Restart=always` + `RestartSec=5`, so the same applies to
  the control plane's own updates. Also: the script's NEG2 assertion read the CP's
  version map, which was stale (v2) even though the units were dead — it must assert
  the units are `active` again. **Fix round 4 dispatched** (Task `task_1e6c6c64c2b9` /
  Dispatch `ctx_1310c6f72a73`): `restart_service()` clears the failed/rate-limit state
  (`systemctl reset-failed "$SERVICE"`) before every restart (shared CP/agent wrapper),
  a fake-systemctl rate-limit regression test, a strengthened NEG2 (units active before
  stopping), and the README note. The box was fully cleaned (units, user, DB, dir);
  after fix 4 the coordinator re-runs the box script and expects C1-C4 + NEG1 + NEG2
  all green.
- **Fix round 4 delivered `0abfabe`**: `restart_service()` now runs
  `systemctl reset-failed "$SERVICE"` (best-effort) before every restart — the shared
  CP/agent wrapper, so a crash-looping new binary can no longer leave systemd's start
  rate limit blocking the rollback restart; `TestWrapperClearsRateLimitOnRollback`
  models the rate limit with a fake systemctl (fails without the fix); the script's
  NEG2 now asserts both statuses record `rolled_back` **and** both units are `active`
  with a fresh v2 heartbeat before stopping them; the interaction and the
  `StartLimitIntervalSec`/`RestartSec` residual are documented. The box re-run is in
  flight. **Follow-up:** because `deploy/gotham-update.sh` is shared, re-run
  `deploy/verify-systemd.sh` (BE-9.1) on the box after this lands to confirm no
  regression.
- **Box re-run after fix 4:** NEG2's rollback assertions now pass (`rolled_back` recorded,
  both units `active`), but the fresh-heartbeat check failed for two separate reasons,
  both confirmed on the box: (1) a **script bug** — Go marshals `time.Time` with
  nanosecond precision and Ubuntu 22.04's Python 3.10 `fromisoformat` rejects 9-digit
  fractions, so `node_heartbeat_epoch` always returned 0 (a false failure); (2) a **real
  product gap** — the agent's failed-attempt backoff (N5) is in-memory only, so after a
  rollback the restarted agent starts with an empty backoff map, re-polls the same
  broken release (the rollout is still active), re-applies it and crash/rollback-loops
  forever (the agent CLI also has no reset path). **Fix round 5 dispatched** (Task
  `task_cdb78a17cdd0` / Dispatch `ctx_9e31dc5347ca`): robust timestamp parsing (+ a
  self-check), backoff seeding from the durable `rolled_back` status at startup, a
  `gotham-agent update reset` operator retry path, a NEG2 stability window (no repeat
  rollback), and tests for both.
- **Pre-merge cross-check:** `deploy/verify-systemd.sh` (BE-9.1) re-run on the box with
  the fix-4 wrapper (`reset-failed`) → **exit 0, all checks passed** (section 1-6,
  real-chain Go ordering, lock-symlink negative), so the shared wrapper change does not
  regress the control-plane chain.
- **Fix round 5 delivered `13766c9`**: the script parses Go's 9-digit RFC3339 by
  truncating to 6 digits (with a startup self-check) so Python 3.10 works, NEG2 adds a
  30s quiet window (wrapper status timestamps and systemd restart counters unchanged —
  proving no re-apply loop), and the agent now **seeds its per-version backoff from the
  durable `rolled_back`/`rollback_failed` status at startup** (30 min, bounded) plus an
  operator retry path `gotham-agent update reset` (clears the pending marker, the
  root-owned status as root, and an agent-owned retry marker the running agent
  consumes), with tests (`TestAgentUpdaterSeedsBackoffFromRolledBackStatus`,
  `TestAgentUpdaterAppliesAfterReset`). The box re-run is in flight; on all-green:
  Claude round 3 (delta), merge PR #72, then re-verify BE-9.1's script once more and
  start INFRA-9.1 (release pipeline).
- **Box proof after fix 5: `deploy/verify-agent-update.sh` exit 0, ALL CHECKS PASSED**
  on the Linux box (C1 baseline + C4 + C1 rollout with the CP current + C3 convergence +
  C2 wrapper `ok`/`.old` + byte-identical planted install + NEG1 tampered-asset refusal
  + NEG2 `rolled_back`, both units active, both fresh heartbeats, a 30 s quiet window
  with no re-apply and no further restarts, agents back on v2.0.0). The coordinator's
  three merge confirmations are therefore proven on real systemd. **Nit found while
  cleaning up:** a *failed* run's trap left the scratch dir (only `status-*`) and the
  scratch user behind (the all-green run cleaned up fully) — a test-script robustness
  item to record/fix, not a product issue. The box is now clean (no units, users, dirs,
  or scratch DBs; the real services are active). Claude round 3 (delta
  `92b8026..13766c9`) is in flight; on MERGE-GO: merge PR #72, then INFRA-9.1.
- **Round-3 verdict (Claude Code, after the all-green box run): FIX-FIRST** — the three
  box-forced fixes are verified correct (`restart_service`/`reset-failed` covers all four
  restart paths in the shared wrapper; the startup backoff seeding only applies to a
  rolled-back version newer than the running one, lasts 30 min, is cleared by the reset
  path and never holds back a newer release; the NEG2 assertions cannot pass on stale
  state) and N1-N5 show no regressions. **New MEDIUM:** `gotham-agent update reset` runs
  as root and writes the retry marker with `os.WriteFile` into the agent-owned
  `/var/lib/gotham-agent`, so a planted symlink makes root overwrite an arbitrary file
  (reviewer repro: a root-only victim became `"retry\n"`) and a FIFO hangs root — the
  same class BE-9.1 closed. LOWs: the seeded backoff never grows across restarts (flat
  30 min), the "no release" answer flips 503→502, reset ignores the unit env/agent.env
  paths, the verify script doesn't pad <6-digit fractions (~1 in 10,000, fails safe),
  and the coordinator's failed-run cleanup nit (scratch dir/user left behind). **Fix
  round 6 dispatched** (Task `task_d40e58274b1b` / Dispatch `ctx_1f24e4a24a90`):
  temp-file+rename/no-follow marker writes with symlink+FIFO tests, plus the meaningful
  LOWs (escalating or documented backoff, consistent error, env-aware reset, padded
  parsing, idempotent cleanup). Then: one more box run (cheap insurance), Claude round 4,
  merge.
- **Fix round 6 delivered `dbf8da0`**: the retry marker is written with a temp file +
  rename (O_EXCL, no-follow) so a planted symlink cannot make root truncate a target and
  a FIFO cannot hang root (symlink/FIFO/normal tests fail on the old write); L1 persists
  the failed-attempt count in an agent-owned `update.backoff` so the seeded backoff
  escalates 5m/10m/20m/40m/1h instead of flat 30m; L2 negative-caches a no-release
  result so update-all stops flipping 503/502; L3 `reset` loads `/etc/gotham/agent.env`
  to resolve the service's paths; L4 pads/truncates any timestamp fraction to 6 digits;
  L5 makes the verify-script cleanup forceful and ordered. A follow-up correction (6b)
  records a failure only on a confirmed rollback so a healthy staged update leaves no
  `update.backoff` and the C3 snapshot stays byte-identical. The box re-run is in flight;
  on all-green: Claude round 4 (delta) and merge.
- **Box proof after fix 6 (second all-green): `deploy/verify-agent-update.sh` exit 0,
  ALL CHECKS PASSED** — same C1-C4 + NEG1 + NEG2 set, with the planted-install snapshot
  now excluding the retry/backoff/marker files and the cleanup verified. CI on
  `dbf8da0`: **9/9 SUCCESS**. Claude round 4 (delta `13766c9..dbf8da0`) is in flight;
  on MERGE-GO: merge PR #72, then start INFRA-9.1 (release pipeline: GoReleaser,
  `cmd/signer`-driven manifest signing, `deploy/install.sh`, install/update guides) and
  prepare the mandatory **gate G2** (`code-reviewer` + `security-reviewer` over the
  whole update chain).
- **Round-4 verdict (Claude Code): FIX-FIRST** — the round-3 MEDIUM is verified closed
  (planted symlink/FIFO as root: victim untouched, no hang) and L1-L5 are all fixed
  (backoff escalates 5m/10m/20m/40m → 1h cap, reset clears it, healthy updates leave no
  `update.backoff`; the no-release cache, env-aware reset, padded parsing and forceful
  cleanup all check out), no regressions, sweep clean. **New MEDIUM (R4-M1):**
  `writeRetryMarker` calls `os.Chmod(tmpName, 0o644)` **by path** inside the agent-owned
  dir, so the agent user can swap root's temp file for a symlink and root makes the
  target world-readable (reviewer's inotify repro won at iterations 1804 and 87; a
  polling attacker never won). Fix: chmod on the open handle. LOWs: the C3 snapshot
  excludes retry/backoff files (should assert they are absent on the healthy path),
  the seeded backoff counts restarts even without a new attempt, a nested retry path
  could redirect root's write (document/enforce), quoted `agent.env` values are kept
  literally, and update-all's "release server error" message is wrong when no releases
  exist. **Fix round 7 dispatched** (Task `task_677945198fa5` / Dispatch
  `ctx_71b315d46690`): the handle-based chmod + tests, the C3 absence assertion, the
  attempt-count fix, the README/enforcement note, quote trimming and the accurate
  message. Then: one more box run, Claude round 5 (delta), merge.
- **Fix round 7 delivered `e187e99`**: the retry marker now sets its mode on the file
  descriptor (`tmp.Chmod(0o644)`) and refuses a symlinked parent, with a Linux inotify
  race test that fails at iteration 877 without the fix; C3 asserts
  `update.retry`/`update.backoff` are **absent** on the healthy path; the seeded backoff
  persists the failure time and only bumps the count when the status `at` changed
  (restarts no longer escalate); the nested-retry-path constraint is documented and the
  immediate parent is symlink-checked; `agent.env` values are unquoted; an empty release
  list returns the consistent 503 no-release answer. The final box run is in flight;
  then Claude round 5 (delta) and merge.
- **Final box run on `e187e99`: exit 0, ALL CHECKS PASSED** (C1 baseline, C4, C1 rollout
  with the CP current, C3 convergence + byte-identical planted install + the new
  `C3 no retry/backoff marker after a healthy update`, C2 wrapper `ok` + `.old`, NEG1
  refusal, NEG2 `rolled_back`/active/fresh heartbeat/30 s quiet window/no further
  restarts) with the box left clean (no scratch dirs/users/DBs). CI: 9/9 SUCCESS.
  Claude round 5 (delta `dbf8da0..e187e99`) is in flight; on MERGE-GO: merge PR #72,
  then INFRA-9.1 and the G2 gate prep.

## Settled task (Phase 9): BE-9.1 control-plane self-update (merged `390a2fa`)

**Merged 2026-09-30**: PR #71 (`feat/p9-update`) merged as `390a2fa`, CI **9/9
SUCCESS**, local main fast-forwarded, `go build ./...` exit 0. Seven independent review
rounds (Codex then Claude Code) closed: N1 lock-symlink root truncation, N2
`wrapper_failed` losing the last known good, M1 install ownership
(`protected_hardlinks`), C1 the startup-resume deadlock, H1/H1b/H2 test-safety hazards
(tests mutating a real install as root), and the FIFO hang class; the real-systemd proof
on the Linux box passed for both direct-root and `sudo` runs with the host install
untouched. Per the owner's keep-workers policy the `p9-update` worktree/branch and the
Claude review session were **retained** (not closed) after the merge; the uncommitted
`docs/ORCHESTRATION.md` and `docs/TODO.md` were updated with the merge record.


Launched 2026-09-30 under the Phase 9 autonomy (execution model effort `high`).
Scope from `docs/plan/10-self-update-release.md` (BE-9.1): `internal/updates/`
(`Checker`, `Applier`, `Signer`) + `cmd/signer`, Ed25519 verification with the public
key embedded in the binary, `github.com/minio/selfupdate` swap keeping `<binary>.old`
with rollback, `GET /api/v1/updates/check` / `POST /api/v1/updates/apply`,
systemd-safe restart + healthcheck + auto-rollback, `FEATURE_UPDATES`/`AUTO_UPDATE`,
and a fake-release local-server test (update → new version → rollback → old).

- Run `run_ad93a55a5d2a`; Task `task_08b95da0accd`; Dispatch `ctx_7cb6f07e7a1d`.
- Worker terminal `term_f8bc5322-2793-4051-b0d8-dcfabd85951b` (OpenCode, variant
  **`high`** — changed from `max` at the owner's request), worktree
  `/Users/ndtpro/orca/workspaces/gotham/p9-update`, branch `feat/p9-update` from main
  `7ba71d3`; untracked `opencode.json` pin.
- Status: **fix round 1 in flight** (Task `task_f9c7229daaaf` / Dispatch
  `ctx_0e582ffb0eca`). Round 0 delivered `9149f15` (draft PR #71, CI 9/9 green);
  the independent Codex review **BLOCKED** and a `security-reviewer` subagent
  **FIX-FIRST**-ed with the same blocker: (1) CRITICAL privilege model — the install
  dir must be Gotham-writable for the swap, yet the root-executed
  `deploy/gotham-update.sh` lived in that same writable dir with unrestricted argv
  (compromised CP → root); (2) HIGH a nonzero `systemctl restart` made `set -e` skip
  the wrapper's rollback (review repro) while the CP detached `sudo` and reported
  success; (3) HIGH `minio/selfupdate` uses a shared `.<bin>.new` staging path and a
  two-rename commit (crash gap, concurrent-apply interleave repro), and drops `.old`
  before every commit; (4) MEDIUM checksums optional + the signature not bound to
  release identity (downgrade via forged tag); (5) MEDIUM `GOTHAM_UPDATE_PUBLIC_KEY`
  overrides the embedded trust anchor unconditionally. Fix round 1 moves the binary
  to a Gotham-writable dir with a root-owned wrapper outside it + pinned argv,
  makes rollback unconditional and the outcome durable, replaces the swap with a
  locked/unique-staging/crash-recoverable one, adds a signed manifest binding
  version+arch+digest (checksums mandatory), ignores the env key when an embedded key
  exists, and tightens redirect/dial SSRF. Then re-review, merge, clean up, then
  BE-9.2 (agent remote update).
- **Fix round 1 delivered `51e1a62`** (draft PR #71, CI 9/9 green): binary moved to
  `/var/lib/gotham/bin` (`StateDirectory`), root-owned argument-free
  `/usr/libexec/gotham/gotham-update` wrapper outside any Gotham-writable path,
  `minio/selfupdate` dropped for a flock-serialized, uniquely-staged,
  crash-recoverable swap that keeps the backup until health is proven, a signed
  manifest binding version/channel/arch/file/sha256 (checksums mandatory), the
  embedded public key no longer overridable by `GOTHAM_UPDATE_PUBLIC_KEY`, tighter
  redirect/dial SSRF checks, and a durable status surfaced in `/updates/check`.
- **Round-2 re-review in flight (Codex Task `task_ef9bbe8d48b1` / Dispatch
  `ctx_3c5178f9c773`)**: escalations confirmed so far — (a) CRITICAL the wrapper
  writes `${STATUS}.tmp.$$` with shell redirection into the Gotham-owned
  `RuntimeDirectory`, so a pre-planted symlink makes root truncate an arbitrary
  root-writable file (repro'd with a fake `systemctl`); (b) the sudoers rule does
  not actually pin arguments (sudoers needs a trailing `""`); (c) two serialized
  applies still overwrite the good `.old` before either health check
  (`applier.go:261-268`); (d) crash recovery only runs in `NewService` but systemd
  `ExecStart` points at the missing target after the first rename, so recovery never
  runs; (e) `os.Executable()` resolves to `gotham.old` after the target is renamed
  (Linux container repro), so later rollback/apply derives the wrong path; (f) the
  detached `sudo` stays in the service cgroup, so the restart can kill the wrapper
  before it records status or rolls back (needs real systemd proof). A parallel
  `security-reviewer` re-check independently confirmed (a)/(b) plus a forgeable
  status file (CP can rewrite it; root should own it) and LOW items (detail path
  disclosure, sudoers argument validation, health-URL parse, `wrapper_failed`
  status). Fix round 2 will address all of these; the real systemd install/restart
  proof is to be run on the Linux `gotham` box before merge.
- **Round-2 verdicts:** Codex round-2 re-review **BLOCKED** (5 findings: the symlink
  status clobber, wrapper cgroup death on restart, `.old` loss across two applies,
  unreachable crash recovery with a missing `ExecStart`, `os.Executable()` drifting to
  `gotham.old`) plus extras (sudoers argument pinning, wrappable health-URL check,
  `CheckRedirect` using `via[0]`, `wrapper_failed` never recorded, docs still naming
  `minio/selfupdate`). The parallel security re-review independently confirmed the
  clobber/status/sudoers items and rated the manifest, lock, rollback, key-precedence
  and SSRF fixes **verified**. **Fix round 2 dispatched** (Task `task_4039ff3801f5` /
  Dispatch `ctx_a7aae6e2c675`): root-owned status dir + safe tempfiles, restart-survival
  (`KillMode=process` or transient scope), staged gate + last-known-good backup with a
  shared flock, a gap-free hardlink backup + rename commit (no missing-target window)
  with wrapper-side recovery, a fixed configured target path (never `os.Executable()`
  after a swap), sudoers `""` + argv/env guards, check-response path/notes stripping,
  and `deploy/verify-systemd.sh` for a real-Linux proof to be run on the box.
- **Real systemd proof run by the coordinator on the Linux `gotham` box (systemd
  249, Ubuntu 22.04) against `11e40cb`**, via
  `rsync` to `/tmp/gotham-p9` + `sudo env -u SUDO_USER -u SUDO_UID -u SUDO_GID sh
  deploy/verify-systemd.sh`: **section 1 PASS** (healthy update → the wrapper wrote a
  root-owned `result=ok` status and released the pending marker) and **section 3 PASS**
  (a helper joined the unit cgroup and survived `systemctl restart` with
  `KillMode=process`) — the two properties the reviewers demanded proof for. Section 2
  rolled back correctly (`result=rolled_back`, nonzero exit) but the script's
  assertion is wrong (it `cmp`s `gotham` against `gotham.old`, which rollback has
  already consumed), section 4 SKIPs (no Go on the box; CI covers those tests) and
  section 5 checks the box's pre-existing `/etc/systemd/system/gotham.service` from a
  *different* installation. Two script bugs also found: `run_wrapper` must strip
  `SUDO_USER/SUDO_UID/SUDO_GID` (running the whole script under `sudo` propagates them
  and correctly makes the wrapper drop every `GOTHAM_*` override, so the test seam
  silently used the defaults), and section 5 must check the repo's
  `deploy/gotham.service` (or SKIP a foreign installed unit). A small fix round on
  `deploy/verify-systemd.sh` follows the round-3 verdict.
- **PAUSED 2026-09-30 (owner):** the round-3 review was interrupted mid-run by Codex's
  content policy (*"This content can't be shown. We take extra care with some
  cybersecurity requests"*) — the same session had already needed sandbox approvals for
  `go`/`docker` — and the account is below 25% weekly quota. The owner asked to pause
  and will nominate a different reviewer agent. State: `feat/p9-update` @ `11e40cb`
  (PR #71 draft, CI 9/9), all product findings through round 2 fixed; the real-systemd
  proof on the Linux box passed sections 1 and 3 (root-owned status + cgroup survival)
  and found three **verify-script** defects whose fix is pre-written in
  `be-9.1-fix3-spec.md`; the reviewer terminal
  `term_9308a34b-2693-476b-91d7-b197d8470ea7` stays idle. Outstanding: round-3
  independent review (new agent), the verify-script fix, merge, then BE-9.2.
- **Reviewer switched to Claude Code (owner decision, 2026-09-30).** `claude`
  (Claude Code CLI 2.1.285, authenticated) is now the independent reviewer instead of
  Codex. The round-3 review of `11e40cb` runs headless:
  `claude -p "$(cat be-9.1-fix2-review-spec.md)" --output-format text
  --permission-mode acceptEdits --allowedTools Read Grep Glob Bash Write --add-dir
  /Users/ndtpro/orca/workspaces/gotham` from the `p9-update` worktree, writing
  `be-9.1-fix2-review.md` and printing `ROUND3-VERDICT: <verdict>`. The coordinator
  snapshots `git rev-parse HEAD`/`git status --porcelain` before and after and treats
  any worktree change as a review failure. Codex remains available but is not used for
  this chain (content-policy interruptions on the security review + low weekly quota).
- **Round-3 verdict (Claude Code, 2026-09-30): FIX-FIRST** — four of five round-2
  blockers verified fixed (status clobber, missing-target window, `os.Executable()`
  drift, sudoers argv) and the fifth only for `staged`. Two new HIGH findings, both
  reproduced: **N1** the root wrapper still truncates a Gotham-planted `update.lock`
  symlink (`exec 9>"${LOCK}"`; real-sudo repro truncated a root 0600 file — the
  round-2 bug class moved to the lock path), and **N2** `wrapper_failed` does not roll
  back, so the unproven binary arms `ExecStart`, the gate reopens and the next apply
  hardlinks it over `.old`, losing the last known good. Required also: **M1** README
  install must chown the binary to `gotham` (else `protected_hardlinks=1` makes the
  first apply fail), and the extended `verify-systemd.sh` must pass on real
  Linux/systemd. Ticketed/documented: M2 (staged update after crash/reboot needs
  startup recovery — partly fixed), M3 (`KillMode=process` lets CP `git`/`ssh`
  children outlive stops; upgrade path = transient `systemd-run --scope`), plus LOW
  L1-L4. The worktree was unchanged apart from the pre-existing untracked
  `opencode.json`. **Fix round 3 dispatched** (Task `task_a22f0d37ec29` / Dispatch
  `ctx_96cbe5e00875`) covering N1, N2, M1, L1, L3, M2 startup recovery, the three
  verify-script defects the coordinator reproduced on the box, and the reviewer's
  real-chain/lock-symlink/ownership additions to `verify-systemd.sh`; M3/L2 are
  documented residuals. Then: re-run the systemd proof on the box, Claude re-review
  of the delta, merge, then BE-9.2.
- **Fix round 3 delivered `885643b`** and the coordinator's **full systemd proof on the
  Linux box passed with exit 0** (`sudo sh deploy/verify-systemd.sh`, systemd 249,
  Ubuntu 22.04): section 1 healthy update → root-owned `result=ok` + pending released;
  section 2 failed start → `rolled_back`, previous binary restored (matches the
  pristine copy) and executable; section 3 cgroup survival with `KillMode=process`;
  section 5 repo-unit wiring + foreign unit SKIP + binary-not-installed SKIP + the
  planted `update.lock` symlink negative **PASS**; section 6 **real launch chain PASS**
  (a non-root scratch unit whose main process runs `setsid sudo -n` the wrapper, the
  wrapper restarts that same unit and still records `ok`/`rolled_back`, clears pending
  and restores the binary; the wrapper cgroup was captured inside
  `system.slice/gotham-verify-chain.service`). Cleanup verified on the box (no scratch
  user, unit, sudoers drop-in or temp dir left). CI on `885643b` queued on the runner;
  Claude round-4 delta review (`11e40cb..885643b`) in flight.
- **Round-4 verdict (Claude Code): FIX-FIRST** — the four round-3 merge criteria are
  met (N1 lock symlink, N2 wrapper-failed rollback, M1 ownership, the extended
  `verify-systemd.sh`), but the new M2 startup-resume introduced **C1 (CRITICAL)**:
  `Server.Run` calls `Resume` before listening and `ResumeStaged` **blocks** on the
  update lock that the wrapper holds through the restart + health window, so a healthy
  update never answers health, the wrapper rolls back, the old binary blocks the same
  way and the update ends `rollback_failed` (~60s downtime; reviewer repro in a Linux
  container). Also noted: the verify script's chain "binary" is a shell script so it
  passed on the broken head (needs a Go fixture that calls the real `Resume` before
  listening), the scratch sudoers file is written before `visudo` validates it, and the
  INT/TERM trap cleans up without exiting. **Fix round 4 dispatched** (Task
  `task_1845ae4f2d5f` / Dispatch `ctx_ccf0bcbbba79`): non-blocking `tryFileLock` in
  `ResumeStaged` + a held-lock regression test, the Go chain fixture, the sudoers/trap
  fixes, the stale README bullet, and the wrapper TOCTOU documented as a LOW residual.
  Then: re-run the box proof **with a real Go toolchain** (`GOTHAM_GO`), Claude
  round-5 delta review, merge, then BE-9.2.
- **Fix round 4 delivered `6738f15`**: `ResumeStaged` uses the non-blocking
  `tryFileLock` (no deadlock on the wrapper-held lock) with a permanent held-lock
  regression test, `deploy/verify-chain/main.go` exercises the real
  `NewService`→`Resume`→listen ordering, the scratch sudoers is validated before
  install, the INT/TERM trap exits, the stale README bullet is fixed and the wrapper
  TOCTOU is a documented LOW residual. **Full box proof with a real Go toolchain
  (`/tmp/go/bin/go` installed on the box) passed with exit 0**: section 4 (gate,
  wrapper-failed rollback, resume incl. held-lock, manifest, lock tests), section 5
  (repo wiring, foreign-unit SKIP, lock-symlink negative) and section 6 (**real chain
  with the Go fixture — "the Go Resume ordering was exercised"** — healthy `ok`,
  broken `rolled_back` + restored + pending cleared). One caveat found: running the
  script *through `sudo`* exports `SUDO_USER` into the Go test harness so the wrapper
  drops its test overrides (the box run therefore ran directly as root, which is the
  script's supported mode); Claude round 5 judges whether the harness should strip
  `SUDO_*` too. **Infra note:** the self-hosted Actions runner on the box had died
  (it was started manually with `./run.sh` in a terminal; no systemd unit), leaving CI
  queued; the coordinator restarted it detached as the `gotham` user
  (`setsid nohup ./run.sh`, log `~/actions-runner/runner.console.log`) — it is online
  and running jobs again. The superseded `885643b` runs were cancelled.
- **Round-5 verdict (Claude Code): FIX-FIRST — C1 verified fixed.** The reviewer built a
  scratch copy with only `ResumeStaged` reverted to the blocking lock: the fixed head
  ends `result=ok` in 0 s while the reverted copy ends `rollback_failed`, and
  `TestResumeStagedSkipsWhileLockHeld` fails on the revert and passes with `-race
  -count=3`; the resume-once semantics still hold (one resume, no loop, new LOW: if
  only the wrapper dies mid-health the update stays `staged` until restart/reset —
  fails closed, needs a README line). The **blocker is a test-safety hazard** the
  reviewer missed earlier: `wrapper_test.go` `runWrapper` does not strip
  `SUDO_USER/SUDO_UID/SUDO_GID`, so when the wrapper sees `SUDO_USER` it drops the test
  config but keeps the host `PATH` and acts on the **real install paths**, and
  `TestWrapperIgnoresEnvUnderSudo` uses the real `systemctl` — a plain root
  `go test ./internal/updates` can restart/rollback the host's real Gotham service
  (the reviewer observed two restarts + a rollback on a box with a real install).
  **Coordinator check of the Linux box**: the real `gotham.service` is untouched
  (`NRestarts=0`, active since 2026-09-27) because its binary path is not
  `/var/lib/gotham/bin/gotham`, so the wrapper stopped at the missing-target guard; the
  only artifacts were a spurious `no_backup` status and an empty `update.lock`, both
  removed. Also LOW: the script SKIPs with "no Go toolchain" when Go is present but the
  fixture build fails, and a `sudo` run can pass without Go coverage — the merge gate
  now requires the literal `Go Resume ordering was exercised` line. **Fix round 5
  dispatched** (Task `task_ca708f4a4e64` / Dispatch `ctx_1d3cdad09983`): strip
  `SUDO_*` in the harness, shim `systemctl`/`mv` + skip when a real install exists,
  `env -u SUDO_*` around section 4's Go tests, fixture-build failure ⇒ FAIL, and the
  README LOW line. Then: box proof (root direct + via `sudo`, requiring the literal
  line), Claude round 6 **continuing the same session** (`--continue`), merge, then
  BE-9.2.
- **Fix round 5 delivered `2d49e0c`** (test/script/docs only): `runWrapper` and
  `TestWrapperRejectsArguments` strip `SUDO_USER/SUDO_UID/SUDO_GID`;
  `TestWrapperIgnoresEnvUnderSudo` uses failing PATH shims with a sentinel, asserts no
  real path was created and skips when a real install exists; section 4 strips
  `SUDO_*`, section 6 FAILs with the build error when Go cannot build the fixture and
  requires the literal `Go Resume ordering was exercised` line when Go is present; the
  wrapper-death residual is documented. **Coordinator proof on the box, run via
  `sudo`: exit 0, all checks passed, the literal Go-ordering line present, and the
  host install untouched** (`/var/lib/gotham*` byte-identical, `gotham.service`
  `NRestarts=0`, same start timestamp) — H1/H1b closed with real evidence.
- **CI hang found on `6738f15` (run 36683769969): `go test ./...` hung 600s** in
  `internal/updates` — the goroutine dump shows `runWrapper` reading the pending path
  (`wrapper_test.go:133` → `openat` O_RDONLY on a FIFO) from
  `TestWrapperHardensPendingRead/fifo`: the wrapper left the FIFO pending marker on a
  fail-closed exit path and the harness then blocked reading it. The suite passes as
  root on the box but hung on the CI runner (user `gotham`), so the fix must be
  structural. **Fix round 6 dispatched** (Task `task_697a09c5816f` / Dispatch
  `ctx_a2d2d226deec`): guarded/non-blocking harness reads (regular files only), the
  wrapper removes a non-regular pending on every exit path, the CP's pending/status
  reads require a regular file, regression tests for FIFO/symlink at every harness-read
  path, and section 4 of `verify-systemd.sh` runs the **full** `./internal/updates`
  suite (the `-run` filter is why the box runs missed this). Superseded CI runs for
  `2d49e0c`/`6738f15` were cancelled to free the runner. Then: box proof (full suite,
  root + via `sudo`), Claude round 6 on the combined delta `6738f15..HEAD`, merge.
- **Fix round 6 delivered `411cb75`** (CI **9/9 SUCCESS**, mergeState CLEAN; box proof via
  `sudo` exit 0 with the full `internal/updates` suite and host untouched): the harness
  reads every result path through `readRegularOrEmpty` (Lstat + O_NONBLOCK + fstat),
  `StatusStore.Read` uses `readRegularFile` (no FIFO/symlink hang or leak in the CP),
  the wrapper clears a non-regular pending marker on every exit path (EXIT trap,
  fail-closed lock path) while preserving a regular staged marker, deadline regression
  tests were added and section 4 now runs the full suite.
- **Round-6 verdict (Claude Code): FIX-FIRST** — H1/H1b closed (verified on clean and
  planted boxes, root / `SUDO_*` / `sudo -E`, shims proven meaningful), the FIFO hang
  class is structurally gone (each deadline test fails when its fix is reverted), and
  every earlier item (C1, N1, N2, M1, L1, L3, M2, real-chain proof) still holds. **New
  H2 (HIGH, test-only, pre-existing)**: `go test ./internal/server` run as **plain
  root** on a host with a real install constructs the real updates service and its
  startup `Recover` mutates production paths — created a root-owned 0600
  `update.lock` (which would break the real `gotham` user's Apply/Rollback), consumed
  the known-good backup, deleted a staging file and removed a staged pending marker.
  Non-root/CI is unaffected, which is why the sweeps missed it. Reviewer warning:
  don't run `./internal/server` as root on a host with a real install until fixed.
  **Fix round 7 dispatched** (Task `task_9ad68c5051b6` / Dispatch `ctx_ad398d05e365`):
  `TestMain` in `internal/server` (and an audit of other server-constructing packages,
  incl. `internal/e2e`) forcing `FEATURE_UPDATES=false` and temp `GOTHAM_UPDATE_*`
  paths, a guard asserting resolved paths stay in the temp dir, and a planted-install
  snapshot regression. Then: box proof (root + `sudo`, planted-install snapshot),
  Claude round 7 (same session), merge, then BE-9.2.
- **Fix round 7 delivered `a0f7736`** (CI **9/9 SUCCESS**): `internal/server` gained
  `main_test.go` with a `TestMain` that disables `FEATURE_UPDATES` and points every
  `GOTHAM_UPDATE_*` path at a temp sandbox, `TestUpdatePathsSandboxed` fails if any
  resolved path escapes it, `TestSanitizedEnvStripsSudo` pins the round-5 harness fix,
  and the worker audited that no other test package constructs the server/updates
  service. **Coordinator's independent proof on the box**: planted a fake install
  (`/var/lib/gotham/bin/gotham`, `.old`, staging, staged pending, status), snapshotted
  md5+mtime, ran `go test ./internal/server` as **plain root** → exit 0 and the install
  is **byte+mtime identical**; then ran `deploy/verify-systemd.sh` **both directly as
  root and via `sudo`** → both **exit 0, all checks passed** (full `internal/updates`
  suite incl. the FIFO regression, the real-chain Go-ordering line, the lock-symlink
  negative) with the host untouched (`NRestarts=0`). Claude round 7 (continued session)
  in flight; on MERGE-GO: merge PR #71, ff main, keep the review session alive per the
  owner policy, then start BE-9.2.
- **Policy (owner directive, 2026-09-30): never close review workers/terminals — keep
  them alive across rounds to preserve the cached prompt/context.** Reviewer sessions
  are not torn down after a verdict; follow-up rounds continue the *same* session
  (Claude Code: interactive TUI in the worktree, or `claude --continue` in the same
  directory for a headless round; Codex: the idle reviewer terminal stays open). Only
  the worker terminals for implementation tasks are closed at settlement, and even
  then the review terminal stays.

## Settled task (Phase 8): FE-8.1 combined UI (merged `7ba71d3`)

Launched 2026-09-29 under the Phase 8 autonomy. Scope from
`docs/plan/09-advanced.md` (FE-8.1): a previews tab in the application detail
(`GET /v1/applications/{id}/previews`), a `/teams` page (members + invites, roles),
a `/settings/notifications` page (4 channel kinds, write-only secrets, send test),
and a `MetricsChart` component on the server detail
(`GET /v1/servers/{id}/metrics`), porting the design tokens from `docs/design`
(`team-settings.html`, `server-detail.html`, `application-detail.html`), plus
Playwright smoke per group and a rebuilt committed `webdist`.

- Run `run_24889d1574cf`; Task `task_910f7322ad81`; Dispatch `ctx_8442c8ef9b1e`.
- Worker terminal `term_27fb9a0e-c294-490b-9365-c7e23aee9f7a` (OpenCode, variant
  `max` confirmed), worktree `/Users/ndtpro/orca/workspaces/gotham/p8-misc-ui`,
  branch `feat/p8-misc-ui` from main `eea683f`; untracked `opencode.json` pin.
- Status: dispatched, worker running. Then verify, draft PR, independent read-only
  Codex review + a `vue-reviewer` subagent (Vue-heavy diff), fix rounds, merge
  autonomously, then assess the Phase 8 exit criteria (M8).
- **Delivered at `47c12f4`** (pushed), draft [PR #70](https://github.com/justindeelux/gotham/pull/70):
  application-detail **Previews tab** (`/v1/applications/{id}/previews`; 404 hides
  the tab), `/teams` (team CRUD with inline last-owner/personal/non-empty errors;
  members role change + remove; invites with the one-time accept link/token shown,
  never persisted) + `/invites/accept`, `/settings/notifications` (4 kinds,
  per-kind form, enable toggle, send test, write-only masked secrets), and a
  **dependency-free SVG `MetricsChart`** on the server detail (CPU/RAM/Disk I/O/
  Network, 1m/1h/1d step, real points with gaps). Ported from
  `team-settings.html` / `application-detail.html` / `server-detail.html` with the
  existing tokens; no new dependency. Local `go build`/vet/test + `npm run
  type-check`/`build` clean, dist drift clean, `npm run e2e` 14/14 (four new
  Playwright smokes). Reviewer terminal `term_a7b20c56-4d08-4b02-913d-bff93eb768dc`
  (Codex read-only, Task `task_b56ea8a0e211` / Dispatch `ctx_f20e536e1bb4`) + a
  `vue-reviewer` subagent in parallel; then fix rounds / merge. Backend-pending:
  invite email delivery, per-channel event editing, per-resource overrides, metric
  auto-refresh; previews link `http://` (BE-8.1 TLS residual).
- **`vue-reviewer` returned FIX-FIRST** (no blockers): **M1** a failed GET renders
  as a confirmed-empty list (`TeamsPage.vue:573`, `NotificationsPage.vue:489` lack
  `!error`/`loaded` gates); **M2** `loadMetrics` (`ServerDetailPage.vue:180-192`)
  has no stale-response guard — a late 1h response wins after switching to 1m, and
  a request for server A resolving after navigating to B sets B's series (and
  `metricsLoaded`) wrongly; **M3** the same pattern in the team-scoped loads
  (`TeamsPage.vue:253/271/457`, `NotificationsPage.vue:306`,
  `stores/notifications.ts:54-73`). LOWs: error-vs-"loading" text, the invite token
  lingering in the URL after accept, a duplicated `PreviewListEnvelope`, `:key=index`
  in `MetricsChart`, 404→"feature disabled" mapping, duplicate first load, a
  `v-model` + handler double-write, and ignoring the returned `step`. Verified
  clean: no `v-html`/`innerHTML`/`console.*`/`any`, token never persisted, secrets
  masked, API clients match the DTOs, `vue-tsc` clean, dist contains the new chunks.
  Awaiting the Codex verdict to fold into one fix round.
- **Codex returned FIX-FIRST** (`fe-8.1-review.md`): **F1** delayed team reads
  overwrite the selected team's collections (reproduced); **F2** a late metrics
  response overwrites the selected range (reproduced); **F3** a first-time invite
  recipient loses the invite when switching to registration (`LoginPage` drops the
  safe redirect). Mockup/contract/security review otherwise clean; dist zero-drift
  and 14/14 Playwright pass.
- **Fix round 1 dispatched**: Task `task_69286fa340dd` / Dispatch `ctx_4caac9750d78`,
  same worker — F1 (invalidate/clear/guard by team, mutations too), F2 (guard by
  request generation + server ID, apply the range with its points), F3 (preserve the
  safe redirect sign-in↔registration), the MEDIUM empty-vs-error gates, and the LOWs
  (error text, URL scrub, duplicate interface, chart key/type scale, feature-off New
  team, duplicate load, v-model, returned step). Report target
  `fe-8.1-fix1-report.md`. Then a re-review, then merge, then the Phase 8 milestone.
- **Fix round 1 delivered at `403045a`** (`47c12f4..403045a`): team-scoped
  reads/mutations are generation- and team-guarded with the previous team's
  collections cleared on switch (`TeamsPage.vue`, `stores/notifications.ts`); the
  metrics view keeps the applied range with its points behind a generation +
  server-ID guard and clears stale series on a switch (`ServerDetailPage.vue`); the
  safe `?redirect` survives the sign-in↔registration switch via a shared
  `utils/authRedirect.ts` and the spent invite token is scrubbed after acceptance;
  the MEDIUM empty-vs-error gates and the LOWs are fixed. `npm run
  type-check`/`build` clean, dist zero drift, `go build`/vet/test clean, Playwright
  **20/20** (6 new regressions, 5 proven to fail pre-fix). Re-review Task
  `task_55d594064f5e` / Dispatch `ctx_0e86ae7ff0e3` (Codex) + a `vue-reviewer`
  recheck in parallel; on MERGE-GO merge #70, ff main, clean up, then the Phase 8
  milestone (M8) assessment.
- **`vue-reviewer` round 2: MERGE-GO** — all three prior P2s + the MEDIUM/LOWs
  verified fixed. One NEW MEDIUM (recommend fixing before merge): on `/servers/:id`
  navigation the `watch(serverId)` callback doesn't reset `metricsLoading` /
  bump `metricsRequestToken`, so the guarded `finally` never clears the spinner and
  the metrics tab wedges until Refresh — one-line fix. LOWs: mutation `busy` flags
  cleared by a stale completion after a team switch; `safeRedirect` accepts a
  backslash-prefixed path; the invite token remains in auth-page history entries.
  Awaiting the Codex verdict; if all-clear apart from the MEDIUM, do a tiny fix round
  2 then merge.
- **Codex re-review returned FIX-FIRST** (`fe-8.1-fix1-review.md`): all 20 specs +
  builds clean and F2/F3 + the GET-state/LOW fixes closed, but **R1** the F1
  mutation generations remain unguarded (an old A role mutation overwrites a newer
  A→B→A result; stale `finally` clears the new team's spinner) and N1 (LOW) the
  obsolete team-delete failure reappears after a selection change.
- **Fix round 2 dispatched**: Task `task_6e61b9baafd0` / Dispatch `ctx_2885fd1e5f97`,
  same worker — R1 (selection/mutation generation + latest-op token, guard every
  success/error/finally, reset stale spinners, apply to member/invite/channel/delete
  paths; two regressions), the vue MEDIUM (metrics spinner/token reset on
  server navigation), and the LOWs (`safeRedirect` backslash, delete ownership).
  Report target `fe-8.1-fix2-report.md`. Then a final re-review, then merge and the
  Phase 8 milestone.
- **Fix round 2 delivered at `bf4bcb7`** (`403045a..bf4bcb7`): `TeamsPage.vue`
  gained a selection generation + per-subject latest-mutation tokens guarding every
  success/error/finally write for member role/remove, invite create/revoke and team
  delete, with a per-member pending map and `resetTeamContext` clearing generation/
  tokens/pending on switch; `ServerDetailPage.vue` releases
  `metricsRequestToken`/`metricsLoading` on server navigation; `safeRedirect`
  refuses backslash-spelled protocol-relative paths. `npm run type-check`/`build`
  clean, dist zero drift, `go build`/vet/test clean, Playwright **23/23** (two new
  mutation regressions + a backslash-redirect test, all failing pre-fix). Final
  re-review Task `task_e889527c234b` / Dispatch `ctx_4ab17529f245` (Codex) + a
  `vue-reviewer` recheck in parallel; on MERGE-GO merge #70, ff main, clean up, then
  the Phase 8 milestone (M8).
- **`vue-reviewer` round 3: MERGE-GO** — the F1 mutation ownership guard (generation
  + per-subject token + `activeTeamId`), the metrics release and the redirect
  hardening are correct; type-check clean. One MEDIUM (fast follow-up): `inviteBusy`
  is not reset in `resetTeamContext()`, so an invite-create completion that no longer
  owns the context leaves the Create-invite button wedged — add
  `inviteBusy.value = false`. One LOW: team delete lacks a per-subject token (only a
  contrived A→B→A double-delete). Awaiting the Codex verdict; if all-clear apart
  from the one-liner, apply it and merge.
- **Codex re-review returned FIX-FIRST** (`fe-8.1-fix2-review.md`): the member,
  metrics, delete, redirect and token fixes hold and 23/23 specs pass, but the same
  MEDIUM — cancelling a pending invite in A then switching to B leaves B's Create
  invite button permanently loading (`inviteBusy` not reset in `resetTeamContext`).
- **Fix round 3 dispatched**: Task `task_ce52d0a404bf` / Dispatch `ctx_41390fe2a0bb`,
  same worker — add `inviteBusy.value = false` to `resetTeamContext()`, add the
  A-cancel→B no-loading regression, plus the LOW team-delete per-subject token.
  Report target `fe-8.1-fix3-report.md`. Then merge and the Phase 8 milestone.
- **Fix round 3 delivered at `ac5d829`** (`bf4bcb7..ac5d829`): `resetTeamContext`
  clears `inviteBusy` and closes a leftover invite form; `beginMutation` mints from a
  never-resetting monotonic counter (no post-switch token collision); team delete
  owns feedback via a per-team token. Playwright **24/24** (a regression holds A's
  invite POST, cancels, switches to B and asserts B is not loading and issues B's
  request — fails pre-fix). Re-review Task `task_3e249d5575a5` / Dispatch
  `ctx_02eccfa761e8` (Codex) + a `vue-reviewer` recheck in parallel; on MERGE-GO
  merge #70, ff main, clean up, then the Phase 8 milestone (M8).
- **`vue-reviewer` round 4: FIX-FIRST** — the invite-busy/form reset and the
  monotonic token counter are correct, but the new per-team delete token
  **over-guards**: `resetTeamContext` clears `mutationTokens` before the success
  continuation runs, so `ownsFeedback()` is always false on a successful delete and
  the "Deleted team" toast is unreachable (regression vs `bf4bcb7`). Fix
  (one-line reorder): let the real outcome (team gone) win first, then the
  token/generation guard. Awaiting the Codex verdict to fold into fix round 4.
- **Codex round-3 re-review: MERGE-GO** (`fe-8.1-fix3-review.md`) — the invite-switch
  and double-delete races are fixed and 24/24 specs pass; only the same non-blocking
  LOW (a successful team delete loses its success toast, a regression from the
  round-3 delete token).
- **Fix round 4 dispatched**: Task `task_a71559dc8786` / Dispatch `ctx_738e8b855f58`,
  same worker — reorder `ownsFeedback()` so the team-gone outcome wins before the
  token/generation guard (restores the delete toast without clobbering the
  double-delete spinner), with a regression; rebuild `webdist`. Report target
  `fe-8.1-fix4-report.md`. Then a quick focused verification, then merge and the
  Phase 8 milestone.
- **Fix round 4 delivered at `e52d953`** (`ac5d829..e52d953`): `ownsFeedback()`
  reordered so the team-gone outcome wins before the token/generation guard,
  restoring the "Deleted team" toast while still blocking stale failures and a
  superseded double-delete's spinner clear; two regressions added (success toast +
  held A→B→A double-delete). Playwright **25/25**; type-check/build clean; dist zero
  drift. Final re-review Task `task_071d7577cacb` / Dispatch `ctx_ca33b5571b64`
  (Codex) + CI on `e52d953` in flight; on MERGE-GO merge #70, ff main, clean up, then
  the Phase 8 milestone (M8).
- **Codex round-4 review returned FIX-FIRST** (`fe-8.1-fix4-review.md`): the success
  toast is restored and dist zero-drift, but the mirror race remains — an older
  delete failure arriving after the newer success surfaces a stale error on the
  fallback team (the "team gone" shortcut wrongly applies to the error path).
- **Fix round 5 dispatched**: Task `task_e2c01e67745e` / Dispatch `ctx_e6e1efcd9374`,
  same worker — use the team-gone outcome only for the success toast; guard the
  error/alert and spinner by current mutation ownership; add a reverse-order
  regression. Report target `fe-8.1-fix5-report.md`. Then merge and the Phase 8
  milestone.
- **Fix round 5 delivered at `c8c4cea`** (`e52d953..c8c4cea`): `teamGone()` alone
  decides the success toast while the failure alert + pending spinner require strict
  mutation ownership (token + selection generation); a reverse-order regression
  (held old failure after a newer success) fails pre-fix with the stale error.
  Playwright **26/26**; type-check/build clean; dist zero drift. Final re-review Task
  `task_fcc3812b23a7` / Dispatch `ctx_1485cc7b04b3` (Codex) + CI on `c8c4cea` in
  flight; on MERGE-GO merge #70, ff main, clean up, then the Phase 8 milestone (M8).
- **Codex round-5 re-review: MERGE-GO** — the reverse-order stale error is prevented
  by strict mutation ownership, 26 Chromium specs pass, dist zero drift. CI on
  `c8c4cea` then failed the Playwright smoke once for an unscoped test locator
  (member-row `tr:visible` filter matched both the members row and the stale invites
  row — a test-selector bug, not a product defect).
- **Fix round 6 dispatched**: Task `task_bc910a7efe5d` / Dispatch `ctx_036f14a8b987`,
  same worker — scope the member row to the members table and refresh/await it after
  acceptance. Report target `fe-8.1-fix6-report.md`. Then CI green → merge #70, ff
  main, clean up, then the Phase 8 milestone (M8).
- **Fix round 6 delivered at `466a8da`** (`c8c4cea..466a8da`): `TeamsPage.vue` tags
  the members and invites tables with stable `data-testid`s and the spec scopes its
  row helpers inside the matching table (replacing every page-wide `tr:visible`
  filter, incl. the read/mutation race specs). Test-scoping + behavior-neutral
  testids only; rebuilt `webdist` zero drift; Playwright 26/26 (the CI-flaky spec
  repeated 3×). Re-review Task `task_d7deb00739f3` / Dispatch `ctx_dcd2d37cd3d4`
  (Codex) + CI on `466a8da` in flight; on MERGE-GO + green, merge #70, ff main,
  clean up, then the Phase 8 milestone (M8).
- **Final verdicts + merge:** Codex round-6 re-review **MERGE-GO** (table-scoped
  locators preserve the assertions, only two behavior-neutral test IDs added, dist
  zero drift, 26/26 Playwright). **Merged 2026-09-29 (Phase 8 autonomy):** PR #70
  marked ready and merged as `7ba71d3` (head `466a8da`), CI **10/10 SUCCESS** on the
  self-hosted runner; local main fast-forwarded `eea683f..7ba71d3`, `go build ./...`
  exit 0. Cleanup: worker terminal `term_27fb9a0e` closed, `p8-misc-ui` worktree
  removed, branch deleted locally and on origin. **FE-8.1 done — Phase 8 complete.**
  Six fix rounds across the two independent reviewers. Phase 8 residuals are recorded
  in `docs/TODO.md` Phase 8 residuals. Next per `AGENTS.md`: the **phase gate** before
  Phase 9 (Self-update & Release, gate **G2**).

## Settled task (Phase 8): BE-8.1 preview deployments (merged `eea683f`)

Launched 2026-09-29 under the Phase 8 autonomy (CI now on the self-hosted
runner). Scope from `docs/plan/09-advanced.md` (BE-8.1): a `pull_request` webhook
path that deploys the PR branch as a sibling **preview application** on
`pr-<n>-<slug>.<base-domain>` (reusing the Phase 4 orchestrator + Phase 6 proxy),
a `preview_deploys` table (migration 00022), a best-effort PR comment with the
preview URL (new provider method for GitHub/GitLab/Gitea), auto-delete on PR
close (+ orphan sweep), and `FEATURE_PREVIEWS`.

- Run `run_ee97cdb7561b`; Task `task_56ef6d02b04f`; Dispatch `ctx_bcdfcb1262f8`.
- Worker terminal `term_b8ba1f6a-062a-4678-a3c6-dbfe4cb7ae56` (OpenCode, variant
  `max` confirmed), worktree `/Users/ndtpro/orca/workspaces/gotham/p8-previews`,
  branch `feat/p8-previews` from main `8e0f1cb`; untracked `opencode.json` pin.
- **Delivered at `212a345`** (pushed), draft [PR #69](https://github.com/justindeelux/gotham/pull/69):
  migration 00022 `preview_deploys`; `pull_request` deliveries for
  GitHub/GitLab/Gitea clone the base app into a sibling app (branch = PR head,
  host `pr-<n>-<slug>.<domain>`) and deploy via the Phase 4 orchestrator/proxy;
  PR comment via a new `CreatePullRequestComment` provider method; delete the
  sibling on close + base-app-delete hook + hourly TTL sweep; hooks install
  `push, pull_request`; `FEATURE_PREVIEWS`. Local build/test/lint + sqlc/proto
  checks clean; gated e2e `TestP8PreviewLifecycle` (open→running→redelivery dedupe
  →close→sibling deleted) passed locally; CI green. Reviewer terminal
  `term_f2bc7262-62ed-4bd1-914a-cd7e199f4c4b` (Codex read-only, Task
  `task_d518e6b13439` / Dispatch `ctx_38c2248f6101`) + a `security-reviewer`
  subagent in parallel; then fix rounds / merge. Worker-flagged residuals:
  previews serve HTTP-only (the base app's wildcard cert intent is not cloned onto
  the sibling — no per-PR certs), the badge comment reflects the queue decision
  not the terminal deploy state, base and preview share storages, and no live Git
  host PR/comment was exercised.
- **`security-reviewer` returned FIX-FIRST**: **H1** — a preview sibling reuses the
  base's `ProviderKeyID`, and the normal authenticated `DELETE /applications/{id}`
  path detaches that shared deploy key from the Git host, breaking the base app's
  (and every preview's) clone; fix by marking previews and skipping host detach (or
  per-preview keys). **M2** previews clone the base's sealed secrets + shared
  storages and are served on a public host → repo-write → secret-read/data-corruption
  jump; fix by not copying secrets/storages and gating fork PRs. **M3** no cap on
  live previews (resource exhaustion). **M4** orphans when best-effort teardown
  fails (sweeper only walks `preview_deploys`). LOWs: `reopened` dedupe by commit
  SHA drops the reopen; delivery-ID dedupe uses an unsigned header. Verified clean:
  signature/tenancy/host-derivation/SQL/comment safety. Awaiting the Codex verdict
  to fold into one fix round.
- **Codex returned FIX-FIRST** (`be-8.1-review.md`): **F1** GitLab hooks don't
  subscribe to MR events; **F2** PR claims on the base app's `(application_id,
  commit_sha)` ledger break reopen/other-PR/**push** at the same SHA; **F3** a
  synchronize during an active deployment is marked handled but lost; **F4** a
  failed binding write leaves the sibling outside all cleanup; **F5** base deletion
  cascades the bindings away when preview teardown fails; **F6** the TTL sweep
  deletes still-open PRs (should be orphan-only); **F7** system teardown retains the
  copied private-key row.
- **Fix round 1 dispatched**: Task `task_f2ac1a4e0531` / Dispatch `ctx_8c8de0ad3182`,
  same worker — F1–F7 + security H1 (deleting a preview must not detach the base's
  shared remote key) + M2 (don't copy sealed secrets/storages; reject fork PRs) +
  M3 (cap live previews) + M4 (orphan sweep) + LOWs. Report target
  `be-8.1-fix1-report.md`. Then a re-review, then merge.
- **Fix round 1 delivered at `ab8e076`** (`212a345..ab8e076`, 31 files): PR
  idempotency moved off the push `webhook_events` ledger onto a new
  `preview_deliveries` table (00023; start keyed by signed app+PR+head, close by
  PR, cleared on close so reopen-at-same-SHA works); a busy synchronize releases
  its reservation and answers **503** (retryable) instead of being dropped;
  bindings persist before queueing with sibling compensation/recovery; base
  deletion aborts on cleanup failure; the sweep is orphan-only over `is_preview`
  apps; teardown deletes the local private-key row but never the base's shared
  remote key; previews copy plain env only (no secrets/storages) and fork PRs are
  rejected; live previews are capped; GitLab hooks subscribe to
  `merge_requests_events`. Local build/test/lint + sqlc/proto clean; full gated e2e
  (151s) + `TestP8PreviewLifecycle`/`TestP4WebhookAutoDeploy` pass; CI/E2E green.
  Re-review Task `task_05537a3543ef` / Dispatch `ctx_fb37468cb8d8` (Codex) + a
  `security-reviewer` recheck in parallel; on MERGE-GO merge #69, ff main, clean
  up, then FE-8.1.
- **`security-reviewer` recheck: FIX-FIRST** — H1 and M2 **fixed** (teardown keeps
  the shared remote key, local key removed; previews copy plain env only; forks
  rejected). Remaining: **M3 cap is read-then-write (not concurrency-safe)** — N
  concurrent distinct-PR deliveries overshoot the cap; fix with one transactional
  per-app-locked insert (`SELECT … FOR UPDATE` / advisory lock) or refuse on the
  insert. LOWs: a failed close leaves the preview live (persist a close intent the
  sweep retries); sweep/already-deleted close don't `ClearPreviewDeliveries` so a
  reopen at the same SHA is silently skipped; fork detection fails **open** when
  `head.repo` is absent (and GitLab zero project ids). INFO: assert `IsPreview` in
  `DeleteSystemApplication`. Awaiting the Codex re-review to fold into fix round 2.
- **Codex re-review returned FIX-FIRST** (`be-8.1-fix1-review.md`) matching the
  security findings plus one new: **N1** an A→B→A PR head sequence is permanently
  suppressed (the fix reserved every historical `(app,PR,head_sha)`); **N2** a
  transient close-ledger cleanup failure permanently blocks reopening; **N3** the
  live-preview cap is bypassed by concurrent distinct-PR deliveries.
- **Fix round 2 dispatched**: Task `task_080eedfeef32` / Dispatch `ctx_d71bc05dc6a2`,
  same worker — N1 (dedupe only against the live binding/in-flight head, signed-body
  authority, atomic per-PR transition, historical SHAs not permanent), N2 (already-
  deleted close retry clears the ledger and propagates clear errors), N3
  (transactional per-app-locked quota reserve), + the 3 hardening items (persist a
  close intent the sweep retries; fork detection fail-closed on missing `head.repo`
  and GitLab zero project ids; `IsPreview` assert). Report target
  `be-8.1-fix2-report.md`. Then a final re-review, then merge.
- **Fix round 2 delivered at `9c426be`** (`ab8e076..9c426be`, 16 files):
  `preview_deliveries` is now an **expiring in-flight lease** claimed in one
  transaction that locks the base application row; a start dedupes only against the
  live binding's current head or a lease for that exact head (A→B→A deploys again,
  concurrent identical deliveries still dedupe); the 5-preview cap is enforced
  transactionally (concurrent distinct-PR opens cannot overshoot); close completion
  is atomic (binding deleted + ledger cleared, retried on the already-deleted path
  with errors surfaced); a failed teardown persists a `closing` state the sweep
  re-attempts; fork detection fails closed on missing `head.repo` / absent GitLab
  project ids; `DeleteSystemApplication` refuses non-preview apps. Local build/vet/
  lint + sqlc/proto clean; full gated e2e (153.9s) + real-Postgres concurrent cap
  test pass; CI/E2E green. Final re-review Task `task_db3b6ba73b8e` / Dispatch
  `ctx_0fb3e02f2ab2` (Codex) + a `security-reviewer` recheck in parallel; on
  MERGE-GO merge #69, ff main, clean up, then FE-8.1.
- **`security-reviewer` recheck: FIX-FIRST (1 MEDIUM)** — cap/N2/close-intent/fork
  all PASS (the transactional per-app lease holds the cap; close is atomic; failed
  teardown persists `closing` and the sweep retries; fork fails closed). **M-1**:
  in `store/previews.go:132-136` the head-equality duplicate case precedes the
  `closing` case, so reopening a PR at the unchanged head while a close is
  in-flight/失败 answers `Duplicate` (200) instead of `Retryable` (503) and the host
  never redelivers; fix = move the `closing` case above (or exclude `closing` from
  duplicate) in `store/previews.go` + the mirrored fake + a regression. Residual
  **R-1** (disclosed): a `synchronize` racing a `close` can recreate the sibling via
  the `ErrNotFound` branch and resurrect a closed PR's preview; guard the recreate
  with a latest-event/state check. LOWs: `CountLivePreviews` counts the claimed PR's
  own lease (can falsely hit the cap on a new-head sync) — exclude the current PR
  when not live; a lease can linger 15 min if the final `UpsertPreview` fails.
  Awaiting the Codex re-review to fold into fix round 3.
- **Codex re-review returned FIX-FIRST** (`be-8.1-fix2-review.md`): N1/N2 and the
  baseline N3 burst fixed, but **N4** a same-SHA reopen while a failed teardown is
  pending is lost (`closing` checked after head-equality → `Duplicate`), and **N5**
  an expired running lease can promote a sixth live preview (the worker's binding
  promotion is not fenced against lease ownership/expiry).
- **Fix round 3 dispatched**: Task `task_c7ee7bc4226c` / Dispatch `ctx_afe0986aad02`,
  same worker — N4 (check `closing` before current-head dedupe in the store claim +
  fake, with a regression), N5 (fence binding promotion against the reservation ID
  and a valid lease under the quota lock; compensate and return retryable for a
  stale worker), R-1 (guard the `ErrNotFound` recreate against a newer close so a
  synchronize cannot resurrect a closed PR's preview), and LOW (exclude the current
  PR from `CountLivePreviews` when not live). Report target
  `be-8.1-fix3-report.md`. Then a final re-review, then merge.
- **Fix round 3 delivered at `5371ab1`** (`9c426be..5371ab1`, 9 files): the claim
  checks `closing` before current-head dedupe (same-head reopen during a pending
  close → retryable); every binding promotion goes through a new **fenced store
  write** under the same base-application lock that verifies the worker's claim
  lease exists and is unexpired, refuses `closing` bindings and re-checks the
  quota, so an expired worker cannot promote a sixth live preview (compensates its
  sibling, 503); a close completed during a racing synchronize cannot be
  overwritten back to active; the quota excludes the claiming PR's own in-flight
  lease. Local build/vet/lint + sqlc/proto clean; full gated e2e (157s) + new `-race`
  service/Postgres regressions pass; CI/E2E green. Final re-review Task
  `task_607519e54262` / Dispatch `ctx_9896bbe38c26` (Codex) + a `security-reviewer`
  recheck in parallel; on MERGE-GO merge #69, ff main, clean up, then FE-8.1.
- **`security-reviewer` round 3: FIX-FIRST (R-1 incomplete)** — N4/N5 genuinely
  fixed (closing-first claim; every promotion path routes through the fenced
  `WritePreviewBinding` that verifies the lease and re-checks the cap under the
  base-app lock). But R-1's fence does **not** serialize with close **completion**:
  `MarkPreviewClosed`/`MarkPreviewClosing` don't take the base-application
  `FOR UPDATE` lock, so under READ COMMITTED a concurrent close can commit between
  the fence's lease read and its `UpsertPreviewDeploy` and be overwritten back to
  active (reproduced; bounded — the orphan sweep clears it). Fix: take the same
  `SELECT id FROM applications WHERE id=$1 FOR UPDATE` in the close path. LOWs: a
  refused final promotion returns 200 with a lingering lease (not a cap bypass); the
  user-facing sibling-delete query deletes a binding without clearing
  `preview_deliveries`. Awaiting the Codex re-review to fold into fix round 4.
- **Codex round-3 re-review returned FIX-FIRST** (`be-8.1-fix3-review.md`): **R-1
  remains open (HIGH)** — a close can complete between the fence's reads and its
  unconditional upsert (close/intent paths don't take the base-app lock); **N6
  (MEDIUM)** — `now()` is transaction-start time, so a lease that expires while
  waiting for the lock is accepted.
- **Fix round 4 dispatched**: Task `task_8ab54848774f` / Dispatch `ctx_2266efc4ead8`,
  same worker — R-1 (serialize close-intent + atomic close completion + no-binding
  close + sweep with the same base-app lock, consistent ordering; real concurrent
  DB regression) and N6 (validate the lease against the current time after the lock
  via `clock_timestamp()`; regression), plus LOWs (503 on a refused final
  promotion; clear `preview_deliveries` on the sibling-delete path). Report target
  `be-8.1-fix4-report.md`. Then a final re-review, then merge.
- **Fix round 4 delivered at `fe4ba55`** (`5371ab1..fe4ba55`, 8 files): every close
  and ledger-clear path (close intent, atomic completion, no-binding clear, sweep
  retry, user-facing sibling delete) now takes the **same base-application row lock**
  as claim/promotion with one consistent ordering, so a close that starts during a
  promotion waits then wins and a completed close can never be overwritten; the
  sibling delete clears the PR ledger; lease/quota validity uses `clock_timestamp()`
  after the lock (a lease that lapses while waiting is refused); a refused final
  promotion is non-2xx. Local build/vet/lint + sqlc/proto clean; full gated e2e
  (184s) + new `-race` real-concurrent regressions (close-inside-gated-promotion,
  lock-wait lease expiry; counter-proved) pass; CI/E2E green. Final re-review Task
  `task_d469f8563332` / Dispatch `ctx_638718726f33` (Codex) + a `security-reviewer`
  recheck in parallel; on MERGE-GO merge #69, ff main, clean up, then FE-8.1.
- **`security-reviewer` round 4: MERGE-GO** — R-1/N6 and both LOWs independently
  verified fixed (counter-proofs reproduce the old failures when the locks are
  reverted), lock ordering is deadlock-free, no regression. F-1 (LOW, pre-existing,
  unchanged path): a no-binding close can miss a binding a racing intermediate
  promotion materializes (narrow; follow-up); F-2 (LOW, latent):
  `UpsertPreviewDeploy` remains an unlocked exported binding-write path (test-only
  callers today); F-3 INFO. Awaiting the Codex verdict to confirm before merging.
- **Final verdicts: MERGE-GO** (Codex `be-8.1-fix4-review.md` + `security-reviewer`
  round 4 — R-1/N6 + both LOWs fixed with counter-proofs, no deadlock, no new
  finding). **Merged 2026-09-29 (Phase 8 autonomy):** PR #69 marked ready and merged
  as `eea683f` (head `fe4ba55`), CI 9/9 SUCCESS; local main fast-forwarded
  `8e0f1cb..eea683f`, `go build ./...` exit 0. Cleanup: worker terminal
  `term_b8ba1f6a` closed, `p8-previews` worktree removed, branch deleted locally and
  on origin. **BE-8.1 done** (four fix rounds — the webhook/lease lifecycle drew
  deep concurrency review). Residuals: previews serve HTTP-only (no per-PR cert);
  the badge comment reflects the queue decision; base and preview share no
  secrets/storages but the pre-existing F-1 no-binding-close race and F-2 unlocked
  `UpsertPreviewDeploy` remain follow-ups; live Git-host PR/comment and wildcard TLS
  unexercised. Next: **FE-8.1 combined UI** — the last Phase 8 package.

## Settled task (Phase 8): BE-8.3 notifications (merged `8e0f1cb`)

Launched 2026-09-29 under the Phase 8 autonomy. Scope from
`docs/plan/09-advanced.md` (BE-8.3): `notification_channels` table (migration
00021) with sealed secret config, team + optional resource scope; a `Notifier`
interface with Discord/Slack webhook, Telegram bot and SMTP email implementations;
an asynchronous bounded dispatcher; narrow hooks at the deploy terminal transition
and backup completion; team-scoped redacting CRUD routes; `FEATURE_NOTIFICATIONS`.

- Run `run_d73d043aa0fc`; Task `task_1fa6dab0fd6e`; Dispatch `ctx_2d4f985377dc`.
- Worker terminal `term_c7bbee94-5324-4b24-9cd8-4d9622d07e59` (OpenCode, variant
  `max` confirmed), worktree `/Users/ndtpro/orca/workspaces/gotham/p8-notify`,
  branch `feat/p8-notify` from main `cd3840b`; untracked `opencode.json` pin.
- Status: dispatched, worker running. Then verify, draft PR, independent read-only
  Codex review (+ a `security-reviewer` subagent for the sealed-secret/redaction
  and SSRF-ish webhook surface), fix rounds, merge autonomously, clean up.
- **Delivered at `3fc3032`** (pushed), draft [PR #67](https://github.com/justindeelux/gotham/pull/67):
  migration 00021 `notification_channels` (team FK + optional resource override,
  sealed config, kind CHECK); `internal/notifications` (Discord/Slack webhook,
  Telegram, SMTP email; bounded async dispatcher; redacted read DTO; test-send
  route; sealed via `providers.SealSecret`); terminal deploy hook +
  backup-completion hook (nil-safe); team-scoped CRUD; `FEATURE_NOTIFICATIONS`.
  Local build/test/vet/`-race`/lint + `sqlc-check`/`proto-check` clean; CI green.
  Reviewer terminal `term_fb93f9d5-fff0-46f6-aaf2-15a53317202d` (Codex read-only,
  Task `task_0a51860350ed` / Dispatch `ctx_b4bdfd6cbfe2`) + a `security-reviewer`
  subagent in parallel; then fix rounds / merge.
- **`security-reviewer` returned FIX-FIRST**: **H1** — `internal/notifications/notifier.go:139`
  wraps the raw `http.Client.Do` error (a `*url.Error` whose text includes the
  full URL), so a transport failure logs/returns the Discord/Slack webhook URL or
  the Telegram `/bot<token>/` secret (`service.go:650` log, `service.go:515`
  test-send response); the "never contains the URL" comment is false and
  untested. Fix: unwrap `*url.Error` (`errors.As` → `uerr.Err`) in `postJSON` and
  the `NewRequestWithContext` parse error, + a transport-failure test asserting the
  secret is absent. LOWs: link-local/metadata denylist for outbound hosts
  (169.254.0.0/16), mirror the `providers` random-key fallback when
  `GOTHAM_SECRET_KEY` is unset, validate the Telegram token charset. Verified
  clean: redaction, team authz, parameterized SQL, bounded dispatcher, nil-safe
  hooks. Awaiting the Codex verdict to fold into one fix round.
- **Codex returned FIX-FIRST** (`be-8.3-review.md`): F1 HIGH (delivery errors leak
  webhook URLs/Telegram tokens via `*url.Error`, remote bodies, request-construction
  errors, and the `/test` response), F2 MED (resource-pair CHECK accepts NULL type
  + non-NULL id), F3 MED (PATCH overwrites a secret with its masked DTO value), F4
  MED (SMTP display-name addresses become invalid MAIL FROM/RCPT), F5 MED (flaky
  backup-outcome ordering test, 3/30).
- **Fix round 1 dispatched**: Task `task_daa7945f10e3` / Dispatch
  `ctx_2c29547dbd8c`, same worker — F1–F5 + the security LOWs (link-local/metadata
  outbound denylist, `GOTHAM_SECRET_KEY` random fallback, Telegram token charset).
  Report target `be-8.3-fix1-report.md`. Then a re-review, then merge.
- **Fix round 1 delivered at `72cc9d7`** (`3fc3032..72cc9d7`, 11 files): delivery
  errors classified (host + category/status, no `*url.Error`/remote body) in
  `postJSON` and SMTP; 00021 CHECK rejects half-pairs; masked secret resends retain
  the stored credential; SMTP `MAIL FROM`/`RCPT TO` use parsed bare addresses;
  backup mapping test single-worker/key-based (30/30); link-local/metadata hosts
  refused; random per-process key when `GOTHAM_SECRET_KEY` unset; Telegram token
  charset-validated/path-escaped. Local build/test/vet/lint + `sqlc-check`/
  `proto-check` clean; CI green. Re-review Task `task_0ac98d9909d4` / Dispatch
  `ctx_fac52a640ec5` (Codex) + a `security-reviewer` recheck of the leak paths in
  parallel; on MERGE-GO merge #67, ff main, clean up, then BE-8.1 previews.
- **`security-reviewer` recheck: NOT fully closed / FIX-FIRST** — 3 of 4 leak
  vectors closed (`*url.Error`, reflected body, SMTP relay text), but **HIGH-1**:
  `notifier.go:154` uses `resp.Status`, whose **reason phrase is remote-controlled**
  and can reflect the credential-bearing path into the dispatcher log and the
  `/test` response (fix: use `resp.StatusCode` + a reason-phrase test). LOWs:
  redirects bypass the link-local denylist (`CheckRedirect`/`DialContext` guard),
  SMTP PLAIN auth over opportunistic TLS (pre-existing), token validated trimmed
  but stored untrimmed. Awaiting the Codex verdict to fold into fix round 2.
- **Codex re-review: FIX-FIRST** (`be-8.3-fix1-review.md`) — F2–F5 closed; F1
  still HIGH via the remote HTTP **reason phrase** (`resp.Status` reflects the
  credential-bearing path into the log + `/test`), and adds a LOW: scoped IPv6
  link-local literals (`fe80::1%eth0`) bypass the host check.
- **Fix round 2 dispatched**: Task `task_9187cc5ab649` / Dispatch
  `ctx_88a02d55e792`, same worker — use `resp.StatusCode` + reason-phrase
  regression; strip the IPv6 zone id and reject scoped link-local; redirect
  denylist (`CheckRedirect`/`DialContext`); trim the stored Telegram token; require
  negotiated STARTTLS before SMTP PLAIN auth unless loopback (or document).
  Report target `be-8.3-fix2-report.md`. Then a final re-review, then merge.
- **Fix round 2 delivered at `cfe2d86`** (pushed): non-2xx errors report only the
  numeric `resp.StatusCode` (reason phrase can no longer reflect credentials, with
  hijacked-status tests for the notifier/`/test`/dispatcher log); cross-origin and
  link-local **redirects** refused before replaying the credential; scoped IPv6
  link-local literals (`fe80::1%eth0`) stripped and rejected; configs trimmed
  before sealing; SMTP PLAIN auth requires negotiated STARTTLS except on loopback.
  All local invariants green (`go build`, `go test ./...` with Postgres,
  `go test -race ./internal/notifications/`, vet, lint, `sqlc-check`/`proto-check`).
- **PAUSED 2026-09-29 (owner decision):** GitHub Actions is blocked at the account
  level — every job on `cfe2d86` fails **pre-start** with the billing/spending-limit
  annotation (runs `36536092615` et al., 0 steps); CI was green on `72cc9d7`. The
  owner chose to pause Phase 8 rather than merge without CI. Worker dispatch
  `ctx_88a02d55e792` released and the worker terminal closed; the `p8-notify`
  worktree (`cfe2d86`) and branch `feat/p8-notify` are **kept**, draft
  [PR #67](https://github.com/justindeelux/gotham/pull/67) stays open, `main` is at
  `cd3840b`. **Resume:** owner restores Actions billing → re-run CI on `cfe2d86`
  (or a new head) → independent re-review of the fix-round-2 delta → merge → then
  **BE-8.1 previews** and **FE-8.1 combined UI** (the remaining Phase 8 packages).
  Run `run_d73d043aa0fc` stays bound to the coordinator terminal; create a new
  OpenCode terminal on the `p8-notify` worktree to continue.
- **RESUMED + MERGED 2026-09-29:** the owner installed a self-hosted Actions runner
  and the account billing block was worked around (see the stop boundary and PR
  #68). `feat/p8-notify` was merged with the new `main` (head `e72b154`), CI
  re-ran on the self-hosted runner **9/9 green**, the final fix-round-2 re-review
  (`be-8.3-fix2-review.md`) returned **MERGE-GO** (reason-phrase leak closed via
  `resp.StatusCode`, redirect denylist, scoped IPv6, token trim, SMTP STARTTLS;
  no new findings), and PR #67 was merged as **`8e0f1cb`**; worktree/branch
  cleaned. **BE-8.3 done.** Next Phase 8 packages: **BE-8.1 preview deployments**
  then **FE-8.1 combined UI**.

## Settled task (Phase 8): BE-8.4 server metrics (merged `cd3840b`)

Launched 2026-09-29 under the Phase 8 autonomy. Scope from
`docs/plan/09-advanced.md` (BE-8.4): an append-style `server_metrics` time-series
table (migration 00020) with 30-day retention; agent network + disk I/O from
`/proc/net/dev` + `/proc/diskstats` (proto fields, reported as bytes/sec like
CPU); heartbeat persistence (keeping the existing `servers` row update); and
`GET /v1/servers/{id}/metrics?from&to&step` with `1m/1h/1d` `date_bin` rollup,
team-scoped via the servers route chain; `FEATURE_METRICS` gate.

- Run `run_03ac11391be1`; Task `task_2e2e793db30b`; Dispatch `ctx_2c54772fe667`.
- Worker terminal `term_56a0d54a-ccd2-402d-b87e-d0a757b6c46a` (OpenCode, variant
  `max` confirmed), worktree `/Users/ndtpro/orca/workspaces/gotham/p8-metrics`,
  branch `feat/p8-metrics` from main `8ecdc2c`; untracked `opencode.json` pin.
- Status: dispatched, worker running. Then verify, draft PR, independent read-only
  Codex review, fix rounds, merge autonomously, clean up.
- **Delivered at `fa48686`** (pushed), draft [PR #66](https://github.com/justindeelux/gotham/pull/66):
  migration 00020 `server_metrics` + sqlc insert/`date_bin` rollup/retention
  queries; agent net+disk I/O byte/sec rates over four additive heartbeat proto
  fields (6–9, `/proc/net/dev` + `/proc/diskstats`, loopback/loop/ram excluded,
  whole devices via `/sys/block`); heartbeat appends one row best-effort (previous
  snapshot update kept); hourly 30-day `MetricsSweeper` started/stopped by the
  server; `GET /api/v1/servers/{id}/metrics?from&to&step` (1m/1h/1d, empty buckets
  omitted) behind `RequireAuth,RequireTeam`, team-scoped via `servers.Get`, with
  `FEATURE_METRICS`. Local build/test/vet/`GOOS=linux vet`/lint clean,
  `make proto-check` + `sqlc-check` clean; CI green. Reviewer terminal
  `term_aa1eda25-9ad1-4ea5-8142-941b506bf0a1` (Codex read-only, Task
  `task_48759225b9ae` / Dispatch `ctx_51d0237107b9`) + a `code-reviewer` subagent
  in parallel; then fix rounds / merge.
- **Reviews:** Codex **FIX-FIRST** (`be-8.4-review.md`) — only a clock-dependent
  range/rollup integration fixture (fails during minute `:59`); production paths
  all pass. `code-reviewer` subagent **MERGE-GO** — one actionable MEDIUM (the
  retention `DELETE … WHERE recorded_at < $1` has no usable index → hourly full
  scan) + 3 LOW (zram counted as disk I/O; one malformed `/proc/net/dev` line
  zeroes all net; container/bridge ifaces double-counted).
- **Fix round 1 dispatched**: Task `task_34aa89861be4` / Dispatch
  `ctx_5bf491282002`, same worker — anchor the fixture to an hour boundary; add
  `server_metrics_recorded_at_idx` to 00020; skip `zram*`; skip malformed net
  lines; doc note for the veth ceiling. Report target `be-8.4-fix1-report.md`.
  Then a re-review, then merge.
- **Fix round 1 delivered at `abd84e7`** (`fa48686..abd84e7`, 6 files): hour-anchored
  the rollup fixture (clock-independent); added `server_metrics_recorded_at_idx` to
  00020 for the retention DELETE; agent reads hardened (`zram*` skipped via a
  testable predicate, malformed `/proc/net/dev` lines skipped, container/bridge
  ceiling documented). Local build/`GOOS=linux` build/vet/test/lint +
  `proto-check`/`sqlc-check` clean; CI green. Re-review Task `task_b35d2f4d9250` /
  Dispatch `ctx_69da810fbb24` (Codex) in flight; on MERGE-GO merge #66, ff main,
  clean up, then BE-8.3 notifications.
- **Final verdict: MERGE-GO** (Codex `be-8.4-fix1-review.md` — all 1440 minute
  alignments / 24 `:59` instants pass; index + strict cutoff verified in a scratch
  DB). **Merged 2026-09-29 (Phase 8 autonomy):** PR #66 marked ready and merged as
  `cd3840b` (head `abd84e7`), CI 9/9 SUCCESS; local main fast-forwarded
  `8ecdc2c..cd3840b`, `go build ./...` exit 0. Cleanup: worker terminal
  `term_56a0d54a` closed, `p8-metrics` worktree removed, branch deleted locally and
  on origin. **BE-8.4 done** (one review round). Residuals: live Linux `/proc`
  execution + production-scale index performance + live chart stress are unverified
  locally (Linux CI covers the `/proc` tests; charts need FE-8.1 + a deployed
  node). Next Phase 8 package: **BE-8.3 notifications** (Discord/Slack/Telegram/
  email; event emitter hook; mock-server tests).

## Settled task (Phase 8): BE-8.2 teams & roles (merged `8ecdc2c`)

Launched 2026-09-29 under the extended Phase 8 autonomy. Scope from
`docs/plan/09-advanced.md` (BE-8.2): teams/team_members/invites tables, a
personal team per user (auto-created on register + backfilled for existing
users), `team_id` on applications/databases/services (NOT NULL after backfill)
and nullable on servers, `RequireTeam` + `RequireTeamRole` middleware/RBAC
(owner/admin/read_only), teams + invites CRUD, and active-team resource
filtering (optional `X-Team-Id` header; absent ⇒ the user's personal team, so
pre-teams clients keep working). `FEATURE_TEAMS` gates the surface.

- Run `run_a464184614a0`; Task `task_277b5c5f4583`; Dispatch `ctx_62e1eb6f21a9`.
- Worker terminal `term_71cf47b6-c68d-420c-8178-3ff0b6739d25` (OpenCode, variant
  `max` confirmed), worktree `/Users/ndtpro/orca/workspaces/gotham/p8-teams`,
  branch `feat/p8-teams` from main `a7d3340`; untracked `opencode.json` pin
  (delete before PR prep).
- Coordinator terminal `term_46c4374f-b620-4c9e-8a6c-bc95047cc431` (bound to this
  run). Status: dispatched, worker running. Then verify scope, open a draft PR,
  an independent read-only Codex review (+ `code-reviewer` subagent for the RBAC
  surface), fix rounds, then merge autonomously and clean up.
- **Delivered at `3f427b6`** (pushed), draft [PR #65](https://github.com/justindeelux/gotham/pull/65):
  migration 00019 (teams/team_members/invites + personal-team backfill + `team_id`
  on applications/databases/services NOT NULL, nullable on servers), new
  `internal/teams` package, `RequireTeam`/`RequireTeamRole`/`teamWriteGate`/
  `withTeam` in `internal/server/teams.go`, active-team scoping wired into all
  four resource domains, invite lifecycle (hashed token, returned once; email =
  BE-8.3 stub), `FEATURE_TEAMS` gate. Full `go test ./...` 21 pkgs ok against local
  Postgres; build/vet/lint clean; `webdist` untouched. Reviewer terminal
  `term_749a8c66-12f0-4dda-aa7d-777966ea7c01` (Codex read-only, Task
  `task_a3c61bcd450b` / Dispatch `ctx_9ae061d985ae`) + a `code-reviewer` subagent
  pass in parallel; then fix rounds / merge.
- **`code-reviewer` subagent returned FIX-FIRST**: **H1** — app/database/service
  create/update validate `server_id` with a bare existence check
  (`internal/deploy/repository.go:210` `ServerExists` + `validateServer`, same in
  databases/services repositories), so a team-A user who knows a team-B node UUID
  can bind and then deploy onto team B's host (and it is an existence oracle);
  fix at the shared `ServerExists`/`validateServer` seam with
  `Scope.AuthorizeOptionalTeam`. **M2** — the invite token is in the accept URL
  path and the request logger writes `r.URL.Path`, so it lands in access logs
  (contradicts "never logged"); send it in the body or redact. **M3** — last-owner
  protection is a non-atomic count-then-mutate (TOCTOU; two owners can demote each
  other to zero owners); do it in one transaction with `FOR UPDATE`. LOWs:
  `read_only` can read plaintext DB credentials (`databases` `Credentials`,
  policy call); `RequireTeamRole` no-scope bypass confirmed **not reachable** in
  production; out-of-scope tables (webhooks/private_keys/backups) fail closed;
  stale doc ref in migration 00019. Awaiting the Codex verdict to fold into one
  fix round.
- **CI #65 RED on one job** (`phase 4 end-to-end (GOTHAM_E2E=1)`, 8/9 pass):
  the raw `INSERT INTO applications` in `internal/e2e/p6_traefik_test.go:466`
  (`p6CreateApplication`) omits `team_id`, hitting the new NOT NULL constraint.
  Fix: set `team_id = userID` (personal team id) in that insert and any other raw
  insert into the four resource tables.
- **Codex review returned FIX-FIRST** (`be-8.2-review.md`), exit criterion FAILS
  with **seven HIGH**: **F1** container routes bypass membership+write RBAC;
  **F2** creator fallback lets a demoted/revoked creator mutate team backups and
  webhooks (reproduced 201); **F3** proxy/cert/redirect routes expose+mutate other
  teams' resources (reproduced cert DELETE 204) and global DNS/proxy is open to
  every JWT; **F4** last-owner TOCTOU (reproduced zero owners); **F5** personal-team
  owner invariant breakable via invite+demote; **F6** foreign `server_id`
  assignment (reproduced 201); **F7** team deletion turns private servers world-
  readable + cascades records. Plus M2 (invite token in access log) and LOWs.
- **Fix round 1 dispatched**: Task `task_3266238f7b5f` / Dispatch
  `ctx_ccd1863f1f77`, same worker terminal. Spec
  `/private/var/folders/98/s589r81s23g6t72rnpth6mkc0000gn/T/opencode/be-8.2-fix1-spec.md`
  covers CI + F1–F7 + M2 + LOWs; platform-global proxy/DNS gets a secure-by-default
  `PLATFORM_ADMINS` operator gate (per-application certs/redirects become
  team-scoped). Report target `be-8.2-fix1-report.md`. Then a re-review, then merge.
- **Fix round 1 delivered at `48d0895`** (`3f427b6..48d0895`, 65 files): every
  production sibling route now resolves the active team and authorizes the owning
  resource's team+role before work (containers/backups/webhooks/proxy certs+
  redirects); F4 serializes membership mutations in one row-locked transaction; F5
  freezes the personal owner's membership; F6 authorizes the target server's team
  on create/reassign/deploy; F7 refuses team deletion while it owns rows and makes
  `servers.team_id ON DELETE RESTRICT`; M2 moves the invite token to the body;
  low read-only-credentials + doc comment. Global proxy sync + DNS CRUD now need a
  platform-operator (`PLATFORM_ADMINS` env / admin-role JWT / admin-scoped API
  token; default deny for plain JWT; documented in `README.md` +
  `docs/test-server.md`); per-application cert/redirect management stays
  team-scoped. Local gated e2e 20 pass/1 skip; **CI on `48d0895` fully green**,
  including the previously red e2e job. Re-review Task `task_0aa214ed60c8` /
  Dispatch `ctx_dc3f7d121004` (Codex) + a `security-reviewer` subagent in parallel;
  then merge autonomously.
- **`security-reviewer` subagent returned FIX-FIRST**: F1, F2, F4–F7, M2 and both
  LOWs **verifiably closed**, but **F3 not closed** — finding 1: any authenticated
  user can `POST /v1/tokens {"scopes":["admin"]}` (the token route has no
  role/operator check) and `RequirePlatformAdmin` trusts admin-scoped API tokens,
  so the platform-global surface is still self-service. Fix: gate `admin`-scope
  issuance behind the platform-admin boundary + regression test. **M**: the WS
  handler (`internal/server/ws/handler.go`) subscribes to a container's log
  channel with no team check (pre-existing, but a production sibling reaching a
  team resource). **L**: team-deletion count-then-delete TOCTOU (self-inflicted;
  make it one row-locked transaction like membership mutation). Awaiting the Codex
  re-review to fold into fix round 2.
- **Codex re-review returned FIX-FIRST** (`be-8.2-fix1-review.md`): F1–F5, M2, the
  CI blocker and both LOWs **closed**; **R1 (F6)** rollback bypasses the
  target-server check (`deployApplication` only; rollback → `submit` queues 202 on
  a foreign node) and **R2 (F7)** the deletion emptiness check races with a
  concurrent insert and cascades committed data.
- **Fix round 2 dispatched**: Task `task_828150875a14` / Dispatch `ctx_2813ca82304f`,
  same worker terminal — **A** gate `admin`-scope API-token issuance behind the
  platform-admin boundary (closes F3 self-service bypass); **B** atomic
  count+delete in one row-locked transaction + `RESTRICT` FKs on
  applications/databases/services; **C** WS log-subscribe team authorization;
  **D** move the stored app→server team check to the shared submit boundary so
  deploy+rollback+system deploys all enforce it (legacy NULL preserved). Report
  target `be-8.2-fix2-report.md`. Then a final re-review, then merge.
- **Fix round 2 delivered at `daa8331`** (`48d0895..daa8331`, 22 files): admin
  scope no longer self-mintable (`POST /v1/tokens` refuses it unless the caller
  passes the same platform-operator checker the middleware uses); team deletion
  atomic (`DeleteTeamIfEmpty` locks the team row, count+delete one tx, all four
  resource FKs `ON DELETE RESTRICT`, raced FK → 409); WS log subscribe authorizes
  the node's team per channel (handshake+frame, denied frame, legacy NULL shared);
  the stored app→node team check moved to the shared submit boundary (closes the
  rollback + signature-verified system-deploy bypass, legacy NULL preserved).
  Local gated e2e 20 pass/1 skip; **CI on `daa8331` fully green**. Final re-review
  Task `task_0599e12e8424` / Dispatch `ctx_8ef522c44de2` (Codex) + a
  `security-reviewer` subagent in parallel; on MERGE-GO merge #65, ff main, clean
  up, then BE-8.4 metrics.
- **`security-reviewer` round 2 returned BLOCK**: A/C/D/B and F1–F7/M2 all verify
  **closed** except one **CRITICAL**: the operator gate compares the **raw**
  `req.Scopes` with `slices.Contains(..., "admin")` while `normalizeScopes`
  `TrimSpace`s before persisting, so `[" admin "]` / `["admin "]` / `["\tadmin"]`
  dodge the gate yet are stored as `admin` — any session or read/deploy token can
  still self-mint admin (reproduced 201 on all three). Fix: normalize before the
  gate (share `normalizeScopes` or `TrimSpace` in the predicate) + a padded-scope
  regression. No other findings; no secret logging; uniform refusals. Awaiting the
  Codex re-review to fold into a tiny fix round 3.
- **Codex re-review independently confirmed the same whitespace bypass**
  (reproduced over production routes with a real token service + fresh DB) and
  reported R1/R2/C pass. **Fix round 3 dispatched**: Task `task_eaf6e037e864` /
  Dispatch `ctx_0656793496f1`, same worker — normalize/sanitize requested scopes
  before the platform-operator check (share the canonicalization) + a padded/
  duplicate-scope regression. Report target `be-8.2-fix3-report.md`. Then the final
  verdict → merge.
- **Codex round-2 verdict (final for `daa8331`): FIX-FIRST** (the whitespace
  bypass); it pinned its repro to `daa8331` because the worktree advanced to the
  round-3 fix mid-review. R1/R2/C confirmed fixed.
- **Fix round 3 delivered at `4e6202f`** (`daa8331..4e6202f`, 3 files): exported
  `auth.NormalizeScopes`, canonicalized the requested scopes **before** the
  operator gate and forwarded the same canonical list to the token service, so the
  authorized and persisted values cannot diverge; padded/dup/mixed variants now
  403 for a plain session or read/deploy token; operator still 201; unknown 400.
  CI on `4e6202f` fully green. Final focused re-review Task `task_e91a39254393` /
  Dispatch `ctx_2d590d88ff64` (Codex) + a `security-reviewer` subagent re-running
  the exact bypass; on MERGE-GO merge #65, ff main, clean up, then BE-8.4 metrics.
- **Final verdicts: MERGE-GO** (Codex `be-8.2-fix3-review.md` + `security-reviewer`
  subagent — padded-admin bypass CLOSED, gate and persistence share one canonical
  list, A/C/D/B + R1/R2 + F1–F7/M2 verified). **Merged 2026-09-29 (Phase 8
  autonomy):** PR #65 marked ready and merged as `8ecdc2c` (head `4e6202f`), CI 9/9
  SUCCESS at the exact head; local main fast-forwarded `a7d3340..8ecdc2c`,
  `go build ./...` exit 0. Cleanup: worker terminal `term_71cf47b6` closed (reviewer
  terminals closed after each round), `p8-teams` worktree removed, branch deleted
  locally and on origin. **BE-8.2 done.** Three fix rounds total across two
  independent reviewers (Codex + a security-reviewer subagent). Residuals: legacy
  `team_id IS NULL` nodes/containers/logs stay shared; a pre-fix self-minted admin
  token (none shipped) would need rotation; operators set `PLATFORM_ADMINS` to use
  global DNS/proxy management. Next Phase 8 package: **BE-8.4 server metrics**
  (run sequentially; migrations collide).

## Settled task 3 (Phase 7): FE-7.1 services UI + template gallery (merged `a7d3340`)

Launched 2026-09-29 (owner autonomy). The last Phase 7 work package: `/services`
(mockup `docs/design/services.html` — Compose services + Template gallery tabs),
`/services/{id}` detail, `/templates`, `DynamicForm` + `ComposeEditor` rendered
from the live BE-7.2 schema, the render → **`env`** → create → deploy flow, English
copy + mockup parity, committed `webdist`, and a Playwright smoke.

- Run `run_a6aefad28c6e`; Task `task_ff6aabf10e2a`; Dispatch `ctx_5658f65ee2fc`.
- Worker terminal `term_f9ccbaf1-dcc3-420b-ac73-cf5b378e31f3` (OpenCode, variant
  `max`), worktree `/Users/ndtpro/orca/workspaces/gotham/p7-services-ui`, branch
  `feat/p7-services-ui` from main `9fb7741`; untracked `opencode.json` pin (delete
  before PR prep).
- **Delivered at `dca2a79`** (pushed): `/services` (Compose + gallery tabs,
  import-compose), `/services/{id}` (compose view/edit via PATCH, masked env,
  containers, follow logs, history, start/stop/restart), `/templates`,
  `DynamicForm`/`ComposeEditor`/`ServiceLogs`/`TemplateGallery`/`TemplateWizard`,
  API clients + Pinia stores, router/sidebar, rebuilt webdist, new Playwright
  smoke, `FEATURE_SERVICES` pinned in `ui-e2e.yml`. Backend-pending stubs
  (deploy-step timeline, log download, per-node counts, service TLS) stay explicit.
  Draft [PR #64](https://github.com/justindeelux/gotham/pull/64). Independent
  reviews in flight: Codex read-only Task `task_edf77a2d14b1` / Dispatch
  `ctx_bbd8b7262566`, plus a `vue-reviewer` subagent pass.
- **vue-reviewer (complementary) returned MINOR** — no HIGH/CRITICAL; security
  (no `v-html`, secrets stay in `env`, masked) and reactivity verified sound.
  MEDIUM: (a) `ServiceLogs.vue:167` calls `releaseLock()` before `cancel()` so a
  mid-stream error never cancels the body — cancel before releasing; (b)
  `ServiceLogs.vue` bypasses the axios 401-refresh path (raw `fetch`). LOW:
  index keys on log/env rows, no reload on route-param change in
  `ServiceDetailPage`, stale compose preview during re-render, `scheduleScroll`
  comment, `TextDecoder` not flushed at stream end, one `Record` type alias.
  Awaiting the Codex read-only verdict, then fold both into one fix round.
- **Codex review returned FIX-FIRST** (`fe-7.1-review.md`): live-data/stubs,
  secrets, contracts, DynamicForm, security, mockup parity, dist (two byte-identical
  builds) and e2e all pass; **R1 HIGH** lifecycle calls inherit the 15 s axios
  timeout while the backend is synchronous up to 15 min; **R2 MED** a pending
  re-render lets Create submit the obsolete compose+env (reproduced: old domain
  persisted); **R3 MED** failed deploy-history reads render as confirmed-empty
  (`0 attempts` / `No deploys yet`); **R4 LOW** the services smoke never activates
  the `guardrails` fixture.
- **Fix round 1 dispatched** (owner autonomy, combines Codex R1–R4 + vue-reviewer's
  2 MEDIUM + actionable LOWs): Task `task_447ac21b0468` / Dispatch
  `ctx_8dafb0e87a5b`, same worker terminal. Report target
  `fe-7.1-fix1-report.md`. Then a re-review, then merge.
- **Fix round 1 delivered at `f4833fe`** (`dca2a79..f4833fe`, pushed): R1 15-min
  lifecycle timeout separate from the 15 s reads; R2 invalidate-pending-render +
  request token (blocks nav/create while rendering); R3 per-service
  loaded/loading/error history with unavailable/retry (empty only after a
  successful read); R4 guardrails; Vue `cancel()`-before-`releaseLock`,
  401 refresh, `TextDecoder` flush, stable row ids, route-param reload, scroll
  coalescing, gallery icon. New `web/e2e/services-regressions.spec.ts` (proven to
  fail on the pre-fix build). Full suite 9 passed agent-less / 4 skipped flag-off;
  type-check/build deterministic; dist drift clean. Re-review Task
  `task_076483cb11ca` / Dispatch `ctx_6c5f9ca15a30` (Codex) + a `vue-reviewer`
  subagent pass, then merge.
- **vue-reviewer re-review returned MINOR** — all five targeted fixes (R1/R2/R3,
  ServiceLogs reader corrections, Vue items) verified correct and the e2e
  regressions genuinely assert the fixes. **New MEDIUM:** the detail page's newly
  added `watch(serviceId)` reload has no stale-response guard
  (`ServiceDetailPage.vue:246-266/:413`) — a late `load()` can overwrite
  `composeYaml`/`envDraft` with the previous service's data and `handleSaveCompose`
  could PATCH the wrong service (same class as R2). LOW: overlapping
  `fetchDeploys` can leave a stale error (`stores/services.ts:111`); log-stream
  refresh failure skips the login redirect (`ServiceLogs.vue:161`). Awaiting the
  Codex re-review verdict, then fold into a small fix round 2.
- **Codex re-review returned FIX-FIRST** (`fe-7.1-fix1-review.md`): R1–R4 + every
  Vue item **verified fixed** (9/9 Playwright, 4 regressions proven to fail
  pre-fix, deterministic zero-drift dist); **new HIGH R5** — the added
  `watch(serviceId)` load is unguarded, so a late A response overwrites B's
  editable compose/env and **Save PATCHes B with A's values** (reproduced live:
  `env: {"MARK":"route-a"}` persisted).
- **Fix round 2 dispatched**: Task `task_e26210186695` / Dispatch `ctx_94f80f66e998`,
  same worker terminal: R5 (captured-ID + request-token guards, captured ID for
  the dependent history read, delayed A→B regression) + 2 LOW (overlapping
  `fetchDeploys` stale error; log-stream refresh failure login redirect). Report
  target `fe-7.1-fix2-report.md`. Then a final re-review, then merge.
- **Fix round 2 delivered at `5aa0065`** (`f4833fe..5aa0065`, pushed): `load()`
  captures the route id + a request token, drops obsolete completions before
  touching drafts, uses the captured id for the dependent history read, and both
  saves are gated (`canEditCurrent` + post-await route checks); `fetchDeploys`
  orders concurrent reads per service; log-reader refresh failure calls
  `expireSession()` (login redirect). New R5 regression (pushState/popstate A→B)
  fails pre-fix on the first post-release draft assertion. Full suite 10 passed /
  6 skipped flag-off; deterministic zero-drift dist. Final re-review Task
  `task_6fd3a70db1b7` / Dispatch `ctx_403bdd290245` (Codex) in flight, then merge.
- **Merged 2026-09-29 (owner standing autonomy):** PR #64 marked ready and merged
  as `a7d3340` (head `5aa0065`), CI 10/10 SUCCESS at the exact head; local main
  fast-forwarded `9fb7741..a7d3340`, `go build ./...` exit 0. Cleanup: worker
  terminal `term_f9ccbaf1` closed, `p7-services-ui` worktree removed, branch
  deleted locally and on origin. **FE-7.1 done — Phase 7 complete.** Residuals in
  `docs/TODO.md` Phase 7 residuals. Next per `AGENTS.md` the **phase gate**
  applies: STOP and ask the owner about Phase 8 (Advanced).

## Settled task 2 (Phase 7): BE-7.2 template engine (merged `9fb7741`)

Launched 2026-09-29 (owner autonomy). Scope from
`docs/plan/08-services-templates.md`: a `templates/{slug}/` format
(`template.yaml` metadata + `compose.yaml` with `{{ .field }}` placeholders), a
schema validator + **non-executing** render engine that reuses the BE-7.1
`services.Parse`/deploy path, the first 4 templates (WordPress, Nextcloud, n8n,
Uptime Kuma), and `GET /api/v1/templates[/{slug}]` + a render route.

- Run `run_36ea59b10bbb`; Task `task_5b2bf44cb88f`; Dispatch `ctx_7025652c2c3b`.
- Worker terminal `term_fa6df29b-ed25-4535-96b4-2d82064ab75e` (OpenCode, variant
  `max`), worktree `/Users/ndtpro/orca/workspaces/gotham/p7-templates`, branch
  `feat/p7-templates` from main `9832466`; untracked `opencode.json` pin (delete
  before PR prep).
- Coordinator terminal `term_46c4374f-b620-4c9e-8a6c-bc95047cc431` (rebound to
  this run).
- Status: dispatched; worker running. Verify head + scope, open a draft PR, then
  an independent read-only Codex review, then merge autonomously.
- **Delivered at `0093b39`** (pushed): embedded `templates/{slug}/` format +
  strict non-executing render engine in `internal/templates`, three admin-scoped
  `FEATURE_SERVICES`-gated routes (`GET /v1/templates`, `GET /v1/templates/{slug}`,
  `POST /v1/templates/{slug}/render`), four templates (WordPress/Nextcloud/n8n/
  Uptime Kuma), gated e2e (WordPress renders → deploys → real Traefik). Draft
  [PR #63](https://github.com/justindeelux/gotham/pull/63). Independent review Task
  `task_7c4896bb511d` / Dispatch `ctx_1aec95553b98` (Codex, read-only) in flight.
- **Review returned FIX-FIRST** (`be-7.2-review.md`): the non-execution renderer
  design and determinism are confirmed sound and the full live e2e passed, but 4
  blocking findings: (1 HIGH) secret fields lose the error-redaction boundary
  across render→create→deploy (parser errors + no-Env deploy history);
  (2 MED) malformed/extra metadata YAML accepted; (3 MED) render requests accept
  trailing garbage/oversized bodies; (4 MED) load-time validation doesn't enforce
  the deployable compose subset; plus 2 LOW (no disabled→503 contract; WordPress
  e2e accepts any 302). Reviewer terminal released + closed.
- **Fix round 1 dispatched** (owner autonomy): Task `task_c7f19ad2e895` /
  Dispatch `ctx_0c65c0eceb8c`, same worker terminal, findings 1–6 with
  regressions. Report target `be-7.2-fix1-report.md`. Then a re-review, then merge.
- **Fix round 1 delivered at `5d823a1`** (`0093b39..5d823a1`, 15 files, pushed):
  secret fields now render as `${field}` references with values in a new **env
  map** in the render response so BE-7.1 substitutes/redacts across
  render→create→deploy (regression drives a real failed deploy: returned error,
  502 body, stored deploy error, logs all redacted, `errors.Is` preserved);
  strict single-document metadata; 413 + EOF request-body handling; load-time
  compose-subset validation; per-call `FEATURE_SERVICES=false` → 503; tightened
  WordPress 302/installer assertion. **FE-7.1 must consume the new render `env`
  contract** (documented in `templates/README.md`). Invariants + gated e2e 140.3 s.
  Re-review Task `task_31440b3299eb` / Dispatch `ctx_d35cc505be8b` (Codex,
  read-only) in flight, then merge autonomously.
- **Merged 2026-09-29 (owner standing autonomy):** PR #63 marked ready and merged
  as `9fb7741` (head `5d823a1`), CI 9/9 SUCCESS at the exact head; local main
  fast-forwarded `9832466..9fb7741`, `go build ./...` exit 0. Cleanup: worker
  terminal `term_fa6df29b` closed, `p7-templates` worktree removed, branch deleted
  locally and on origin. **BE-7.2 done.** Next: FE-7.1 services UI + template
  gallery (must consume the render response's new `env` map).

## Settled task 1 (Phase 7): BE-7.1 compose services (merged `9832466`)

Launched 2026-09-29 after the owner authorized Phase 7. Scope from
`docs/plan/08-services-templates.md`: a service = a compose project on one node;
the plan's decision is **agent-side `docker compose` CLI** execution with the CP
storing/versioning the compose YAML.

- Run `run_005a1e52e531`; Task `task_6757f1113145`; Dispatch `ctx_8fe32ec5d464`.
- Worker terminal `term_ffb56b8b-34c0-4d3e-a6b2-5dd691e85c1c` (OpenCode, variant
  `max` confirmed in the footer), worktree
  `/Users/ndtpro/orca/workspaces/gotham/p7-compose`, branch `feat/p7-compose`
  from main `7ccda27`; untracked `opencode.json` model pin (delete before PR prep).
- Coordinator terminal `term_46c4374f-b620-4c9e-8a6c-bc95047cc431`.
- Spec: agent `ComposeService` proto RPCs (`ComposeValidate`, `ComposeUp`,
  `ComposeDown`, `ComposeLogs`, `ComposePs`); `internal/services`
  (parse/validate, env substitution, compose-label→Traefik domain map, CRUD +
  deploy/stop/restart, `FEATURE_SERVICES` kill switch); `services` +
  `service_deploys` forward-only migrations (next number `00017`); sqlc
  regenerate; `internal/e2e/p7_compose_test.go` gated by `GOTHAM_E2E=1`
  (2 services + named volume → ps → per-service logs). Env verified for the
  worker: Docker 29.8 + Compose v5.5.1 present locally.
- Status: **delivered at `602dfc0`** (agent `ComposeService` + `internal/services`
  + migrations 00017/00018 + sqlc + gated e2e; no `web/`). `worker-list` shows the
  dispatch `succeeded`/`completed`. Branch pushed; draft
  [PR #62](https://github.com/justindeelux/gotham/pull/62), CI 9/9 SUCCESS.
  **Main residual gap (honest):** the service domain map is computed/stored/API-
  visible but the Phase 6 proxy generator does not consume service domains yet —
  no Traefik router is pushed for a service `gotham.domain`. Independent review
  Task `task_b063fce7e4ce` / Dispatch `ctx_03b682ed4ba9` (Codex, read-only) in
  flight, with the scope-gap question (finding 1) as its central verdict question.
  **Owner standing directive: coordinate all of Phase 7 autonomously (merge, fix
  rounds, cleanup) without asking back.**
- **Review returned FIX-FIRST** (`be-7.1-review.md`, 11 findings: 5 HIGH, 5 MED,
  1 LOW). Central scope answer: the service-domain Traefik wiring is a **missing
  BE-7.1 deliverable** that must be fixed in this package. HIGH: unredacted env
  in stop/delete/restart errors; volume `name` alias bypasses the `gotham-db-*`
  guard; agent concurrent-RPC document race; lifecycle `UpdateService` overwrites
  a concurrent edit. Reviewer terminal released + closed.
- **Fix round 1 dispatched** (owner autonomy): Task `task_4fe0bebaa4f4` /
  Dispatch `ctx_259908507777`, same worker terminal, all 11 findings + the real
  Traefik Host-header acceptance assertion + regressions. Report target
  `be-7.1-fix1-report.md`. Then a focused re-review, then merge.
- **Fix round 1 delivered at `50cb375`** (`602dfc0..50cb375`, 28 files, pushed):
  all 11 findings fixed with regressions; service domains now route through a new
  `internal/proxy` `ServiceSource` seam with real Traefik Host-header acceptance
  (200/404/200/404); one environment-aware redaction boundary; effective
  volume-name validation; per-project agent serialization; status-only lifecycle
  writes + per-service locks; name-first interpolation; owned/closed gRPC
  connections; drain-before-`Wait` logs + first-read error preservation; bounded
  render/CLI capture; snapshot-based delete; doc fix. **Also fixed a Phase 6
  defect it surfaced** (`internal/proxy/generate.go`: omit an empty `http:`
  section — Traefik rejected it and kept the last route alive). Invariants green;
  gated e2e 118 s (P7 23.6 s real Traefik). Focused re-review Task
  `task_2bdfe00c83cd` / Dispatch `ctx_15c55b0331e3` (Codex, read-only) in flight,
  then merge autonomously.
- **Re-review 1 returned FIX-FIRST again** (`be-7.1-fix1-review.md`): routing +
  Phase 6 empty-`http` fix confirmed correct and real (Host-header 200/404/200/404);
  findings 3/4/6/10/11 fixed; 2/5/7/8 only partially. **5 blocking defects:** R1
  lifecycle lock acts on a stale pre-lock row (queued Restart resurrects a deleted
  project); R2 terminal log errors bypass redaction; R3 quarantine of quiet
  `--follow` opening + CLI-stderr refusal still commits HTTP 200; R4 `Close` on a
  backpressured stream leaks the drain goroutine; R5 drain-before-`Wait` hangs
  cancellation on a quiet descendant. Non-blocking N1 (fixture race), N2 (bounds
  wording). Re-reviewer terminal released + closed.
- **Fix round 2 dispatched** (owner autonomy): Task `task_a3a57475f4d1` /
  Dispatch `ctx_91926572030f`, same worker terminal, R1–R5 + N1–N2 with
  regressions. Report target `be-7.1-fix2-report.md`. Then a second re-review,
  then merge.
- **Fix round 2 delivered at `af27608`** (`50cb375..af27608`, 12 files, pushed):
  R1 lock-before-read (queued delete/restart/deploy regressions); R2 terminal
  log-error redaction boundary; R3 agent validates the selector before the CLI +
  additive acceptance frame (quiet follow opens, refusal can never commit 200);
  R4 stream-owned cancel context (Close unblocks backpressured drain); R5
  cancellation closes pipe drains independently of Send; N1 race-free fixture;
  N2 escaped-length budget + diagnostic-sized failure capture. Invariants green
  incl. `-race`; gated e2e 118.7 s first attempt. Second re-review Task
  `task_66dfdd72793f` / Dispatch `ctx_7de3a110f68d` (Codex, read-only) in flight,
  then merge autonomously.
- **Re-review 2 returned FIX-FIRST** (`be-7.1-fix2-review.md`): R1/R2/R4/R5/N1/N2
  fixed; routing/Phase 6/SQL/volume/interpolation/connection clean. Two blockers:
  **R3 residual** — the `logs` handler writes 200 but flushes only inside the
  chunk loop, so a quiet follow never sends headers (real HTTP client times out);
  **R6 new** — the failed `stream.Send(ready)` path in `agent/compose.go` returns
  without reaping the started CLI (zombie). Re-reviewer terminal released + closed.
- **Fix round 3 dispatched** (owner autonomy): Task `task_c63a52c1deb5` /
  Dispatch `ctx_03b1761d89c4`, same worker terminal, R3 (flush after acceptance +
  real-HTTP quiet-follow regression) + R6 (reap on the failed-acceptance path +
  no-waitable-child regression). Report target `be-7.1-fix3-report.md`. Then a
  third re-review, then merge.
- **Fix round 3 delivered at `4003206`** (`af27608..4003206`, 4 files, pushed):
  `routes.go` flushes the accepted response immediately after `WriteHeader` so a
  quiet follow reaches a real HTTP client at once (refusal still answers its error
  status first); `agent/compose.go`'s failed-acceptance path cancels and reaps the
  started command (follow path given a cancelable command context). Real
  regressions: quiet-follow HTTP test fails 5.02 s pre-fix / passes 0.85 s post;
  acceptance-failure test `Wait4=0,nil` pre-fix / `ECHILD` post. Invariants +
  `-race` green; gated e2e 122.2 s. Third re-review Task `task_bee30d932234` /
  Dispatch `ctx_d418ab553d9b` (Codex, read-only) in flight, then merge autonomously.
- **Merged 2026-09-29 (owner standing autonomy):** PR #62 marked ready and merged
  as `9832466` (head `4003206`), CI 9/9 SUCCESS at the exact head; local main
  fast-forwarded `7ccda27..9832466`, `go build ./...` exit 0. Cleanup: worker
  terminal `term_ffb56b8b` closed, `p7-compose` worktree removed, branch deleted
  locally and on origin. **BE-7.1 done.** Residuals recorded in `docs/TODO.md`
  Phase 7 residuals. Next: BE-7.2 template engine.

## Settled task 1: BE-6.1 R2 correction

- Run: `run_858e742082eb`; coordinator terminal:
  `term_4d3a4f5a-b377-4273-8997-57081a92c6ff` (main checkout).
- Task: `task_88afab865342`; authoritative Dispatch: `ctx_d53cdc2d0d16`
  (replacement via `--retry-of` after the mis-launched `ctx_428ae01c8b99`
  was abandoned with positive idle-tail evidence and zero task turns).
- Worker: operator-created terminal
  `term_c9a8ffba-2eec-48bf-86cb-c2f81b6aa400`, reused after an explicit ownership
  decision; OpenCode session model verified via session export as
  `opencode-go/deepseek-v4.1-flash`, variant `max`.
- Workspace: `/Users/ndtpro/orca/workspaces/gotham/p6-traefik`,
  branch `feat/p6-traefik`; draft [PR #51](https://github.com/justindeelux/gotham/pull/51).
- Status: authoritative worker_done `msg_56bede4b0eb0` verified completed/succeeded.
- Pushed head: `ef297697a3bce9150eed95460d3698347ad7f662`; checkout clean.
- Change: `push` returns a plain error (no touched flag); `pushAndPromote` has no
  abort branch, so EVERY push failure (write, timeout, ping, dial) retains the
  pending record and returns `ErrHistory` wrapping the cause; only successful
  promotion or the explicit restoration path clears pending. New executable
  regression `TestSyncServerPendingRetainedAcrossFailuresRevertsToActive`
  (B-ambiguous -> C-dial-failure -> fresh-service revert targets durable active A,
  never superseded Z). `AGENTS.md`/`CLAUDE.md` diff is GitNexus index-stat refresh
  only. No migration change, no main merge, no SSL/UI scope.
- Validation: local build/vet/tests/lint/web/sqlc/proto/boundaries all exit 0;
  R2 regression set visibly PASS; disposable-Postgres sequencing PASS.
- Exact-head CI on `ef29769` all SUCCESS: push `36367275526`, PR CI `36367277851`,
  PR E2E `36367277863` (headSha verified on all three).
- Product remains BLOCKED on independent review; PR #51 is still draft/unmerged.
- Worker release: `worker-retain` on `ctx_d53cdc2d0d16` (operator-owned terminal,
  no process action); delivery ACKed; no reclaimable terminals; no active task.
- Final report: `/Users/ndtpro/orca/workspaces/gotham/be-6.1-r2-report.md`.
- Evidence: `/Users/ndtpro/orca/workspaces/gotham/evidence-be-6.1-r2/`
  (includes `pre-edit-impacts.txt`, closing the prior provenance gap).

## Settled task 2: BE-6.1 independent final review

- Same run `run_858e742082eb`; Task: `task_4a3830741f18`; authoritative
  Dispatch: `ctx_6475105b1e6e`; worker terminal (fresh, created by Orca):
  `term_c34fea0e-0956-4d04-aa4f-47012850e71e`.
- Reviewer: Codex, requested and effective model `gpt-6-sol` (observed
  turn_started, provider-supported observation). Independent of the OpenCode
  implementation lane. Old reviewer terminal was not touched.
- Status: authoritative worker_done `msg_8d39b7713c04` verified
  completed/succeeded. **Product verdict: PASS. Overall change risk: HIGH.**
- Reviewed exact head `ef29769` (rev-parse + branch + ls-remote + PR metadata
  agree); PR #51 still draft/unmerged, merge state CLEAN; merge-base is main
  `718d19b`. Complete current-main diff: 64 files, +8,681 / -353.
- Dispositions: R1-R5 all resolved; F1-F7 resolved (F7's malformed-filename edge
  downgraded to L2). R2 failure-path audit found no error-specific abort/delete
  path. Two LOW follow-ups: L1 (R2 regression never prepares distinct C content;
  coverage/wording qualification, implementation correct) and L2 (NUL byte in
  final basename escapes early batch validation; confined, privileged-input only).
- Read-only boundary verified: p6 checkout still clean at `ef29769`, no new
  commits; PR head unchanged. Coordinator independently verified exact-head CI
  (push `36367275526`, PR CI `36367277851`, PR E2E `36367277863`, headSha match).
- Worker release: `worker-release` on `ctx_6475105b1e6e` (terminal closed,
  output archived); delivery ACKed; no reclaimable terminals; no active task.
- Final report: `/Users/ndtpro/orca/workspaces/gotham/be-6.1-final-review.md`.

## Settled task 3: BE-6.1 L1+L2 LOW follow-ups

- Run: `run_a89815e53352`; Task: `task_79833468c483`; authoritative Dispatch:
  `ctx_11d55be78260`; worker: retained operator terminal
  `term_c9a8ffba-2eec-48bf-86cb-c2f81b6aa400` (same verified DeepSeek session;
  L1+L2 share the proxy scope so no /compact was needed).
- Branch `fix/p6-l1l2` (from main `89bd579`) in the p6 folder workspace.
- Status: authoritative worker_done `msg_1a7fca1e1098` verified
  completed/succeeded. Pushed head `34550df`; checkout clean.
- Change: L1 distinct-C regression (new pending hash/ID differ from B, active A
  survives, fresh revert pushes A); L2 NUL rejection in `sanitizeProxyPath`
  with batch-rejection + earlier-document preservation regression; R2 report
  wording corrected. `TestSanitizeProxyPath` untouched (its CRITICAL impact was
  webdist name pollution, warned mid-task). `AGENTS.md`/`CLAUDE.md` diff is
  index-stat refresh only.
- Validation: build/vet/unit/lint/web/sqlc/proto/boundaries exit 0; targeted
  regressions visibly PASS with bite (pre-fix failures demonstrated); exact-head
  push CI `36376213679` SUCCESS.
- Worker release: `worker-retain` (operator-owned, no process action); delivery
  ACKed; no reclaimable terminals.
- Report: `/Users/ndtpro/orca/workspaces/gotham/be-6.1-l1l2-report.md`;
  evidence: `/Users/ndtpro/orca/workspaces/gotham/evidence-be-6.1-l1l2/`.
- Coordinator created draft [PR #53](https://github.com/justindeelux/gotham/pull/53).
  Owner chose a quick review first: Task `task_02579f142738`, Dispatch
  `ctx_ad2f93b0b5d4` (Codex `gpt-6-sol`, read-only delta scope) returned
  **MERGE-GO** (`be-6.1-l1l2-review.md`, no blocking findings). PR marked ready
  and merged as `9e7f7da`; local main fast-forwarded, `go build ./...` exit 0.
  Reviewer terminal released, delivery ACKed, no reclaimable terminals.

## Settled task 4: P5 backup LOWs (dedicated worktree, fresh worker)

- Run: `run_4df06f272cf6` (clean relaunch; earlier `run_4779d84e31ee` attempt was
  abandoned with zero source edits after the owner flagged P5-on-P6-terminal
  mixing). Task: `task_91f25a1398e8`; authoritative Dispatch: `ctx_66d3d9bfe89f`.
- Dedicated worktree `/Users/ndtpro/orca/workspaces/gotham/p5-backup-lows`,
  branch `fix/p5-backup-lows` from main `9e7f7da`. Fresh OpenCode terminal
  `term_943bfad8-fb16-49e3-85c2-6e1974b2d92a` + fresh session; model pinned by
  worktree-root `opencode.json` (`opencode-go/deepseek-v4.1-flash`, untracked,
  never committed, kept until merge for follow-up launches).
- Status: authoritative worker_done `msg_df3850b33a0b` verified
  completed/succeeded. Pushed head `bde5ef1` (2 commits: pure-move split +
  LOWs); checkout clean except untracked `opencode.json`.
- Change: tri-state schedule `target_id` (absent=keep, empty=clear, id=replace)
  documented + regressed; service split into core/schedules/targets/execution
  with identical normalized multiset + exported-declaration md5; UpdateTarget s3
  credential parity (rejection + non-credential field-update regressions).
- Validation: build/test/vet/lint exit 0, targeted verbose + disposable-Postgres
  integration PASS, exact-head push CI `36378314889` SUCCESS (headSha verified).
- Worker release: retained external terminal, no process action; delivery ACKed;
  no reclaimable terminals; no active task.
- Report: `/Users/ndtpro/orca/workspaces/gotham/p5-backup-lows-report.md`.
- Coordinator created draft [PR #54](https://github.com/justindeelux/gotham/pull/54).
  Owner chose quick review first: Task `task_3fb650a3fe73`, Dispatch
  `ctx_9e0b79be17c1` (Codex `gpt-6-sol`, read-only delta scope) returned
  **MERGE-GO** (`p5-backup-lows-review.md`, split preserves all 56 declarations,
  both exact-head CI runs green). PR marked ready and merged as `3705260`;
  local main fast-forwarded, `go build ./...` exit 0. Reviewer terminal
  released, delivery ACKed, no reclaimable terminals.

## Settled task 5: BE-6.2 SSL code-only (dedicated worktree, fresh worker)

- Run: `run_0e2895730e47` (created 2026-09-28 after the owner chose BE-6.2
  code-only from the "no Cloudflare/domain" filter: real issuance verification
  stays blocked, everything else is implementable and testable now).
- Task: `task_e5d6670b2f5b`; authoritative Dispatch: `ctx_b3a35a5c7df4`;
  worker terminal: `term_38daecc0-2096-4589-958c-cd75ffdae9e7`
  ("P6 SSL worker", OpenCode). Model pin via worktree-root `opencode.json`
  (`opencode-go/deepseek-v4.1-flash`, untracked, never commit); TUI preview
  verified `Build auto · DeepSeek V4.1 Flash OpenCode Go · max`. First
  heartbeat `delivery_feae8f683f4f` accepted (no rejection), phase
  `investigating`.
- Worktree: `/Users/ndtpro/orca/workspaces/gotham/p6-ssl`, branch `feat/p6-ssl`
  from main `3705260` (Orca created `justindeelux/p6-ssl`, renamed for
  convention). Setup skipped; sqlc v1.31.1, buf and `/usr/local/go` 1.25.7
  available.
- Scope (code-only): dns_providers migration + CRUD with sealed credentials
  (provider allowlist cloudflare/digitalocean); ACME generator extension for
  DNS-01 resolvers alongside the existing HTTP-01 default; wildcard
  `tls.domains` support; HTTPS router + `gotham-https-redirect` activation
  path gated by an explicit predicate (worker must `ask` before inventing a
  state/schema design); provider tokens delivered to the gotham-traefik
  container env only (convergence/repair extended, redacted everywhere); tests
  replace real issuance. Explicitly out of scope: web/, deploy/build flows,
  agent proto RPCs, real issuance claims.
- Launch procedure that worked this time: pin model → `terminal create
  --command opencode` → `terminal wait --for tui-idle` → `task-create` →
  `worker-start --task ... --terminal ...` — the fresh TUI started the turn
  directly and the capability bound on the first heartbeat. If a fresh TUI
  ever reports input accepted with an idle composer, fall back to the P5
  procedure (manual `terminal send` of the task text + Enter, then re-attach
  with `worker-start --terminal`).
- Status: authoritative worker_done `msg_36378736bd4a` verified
  completed/succeeded. HEAD `5e6027326e7c1119fb9f5d314e8c1e7dd9bf2e5f`
  pushed and in sync; checkout clean except untracked `opencode.json`; no PR.
  Exact-head push CI `36383447127` SUCCESS (headSha verified). Deliverable:
  migration 00015 (`dns_providers` sealed + `domain_certificates` intent),
  ACME DNS-01 resolvers + `tls.domains` wildcard, activation predicate gated
  to active cert rows, env-only Traefik credentials with HMAC fingerprint
  drift detection; 108 targeted SSL tests + real-Postgres constraint test +
  `GOTHAM_E2E=1` P6 production acceptance all PASS. Real issuance NOT verified
  (blocked, explicitly not claimed). Report: `be-6.2-report.md`. Worker
  terminal retained for a possible fix round; delivery ACKed. Awaiting owner
  review/merge decision; delete `opencode.json` before PR prep.
- Owner chose review-first. Review Task `task_1be25d46bb23`, Dispatch
  `ctx_40065165a624` (Codex `gpt-6-sol`, read-only delta) returned **FIX-FIRST**
  (`be-6.2-review.md`; reviewer terminal released, delivery ACKed):
  P1 empty deployment secret gives DNS credentials a publicly derivable key;
  P1 wildcard activation does not cover multi-level hosts; P2 provider guards
  and credential rotation are not atomic; P2 strict JSON decoding accepts
  trailing input, oversized bodies and null; plus an audit follow-up to
  sanitize the bootstrap/push error boundary. Fix round dispatched to the
  retained worker session: Task `task_62c65621a6c7`, Dispatch
  `ctx_9f9ace8b553a`; worker resumed on the review file immediately. After the
  fix round: full re-validation, focused re-review of the fix delta, then the
  owner merge decision.
- Fix round delivered at `bb44051` (2 commits: index-stats + fixes), exact-head
  push CI `36405249716` SUCCESS, checkout clean, no PR. Fix-delta re-review
  dispatched: Task `task_6edab66975e1`, authoritative Dispatch
  `ctx_1910c7ea6191`, verdict target `be-6.2-fix-review.md`. First Codex
  dispatches for this re-review failed because the Codex TUI's "Update
  available" prompt swallowed the dispatch input and self-updated the CLI to
  0.158.0, which Orca 1.4.215's readiness check does not recognize
  (`agent_readiness` timeouts); resolved by pinning `@openai/codex@0.157.1`,
  setting `dismissed_version=0.158.0` in `~/.codex/version.json`, relaunching a
  clean terminal and using `worker-start --retry-of`.
- Fix-delta re-review (Task `task_6edab66975e1`, Dispatch `ctx_1910c7ea6191`,
  Codex `gpt-6-sol`) returned **FIX-FIRST** again but narrower
  (`be-6.2-fix-review.md`; F1-F4 resolved, tests/race/vet/CI verified): B1 the
  redaction boundary covered only the `containers.Run` error (Start/Pull/
  Remove/agent-write/ping/revert/Close could still carry credential text) and
  B2 the sanitizer replaced errors with `errors.New`, destroying `errors.Is`
  chains and changing established HTTP status mapping (502/400/404 -> 500).
  Second fix round dispatched to the retained worker session: Task
  `task_0995ee115a63`, Dispatch `ctx_31e299cc7e7e`. Reviewer terminal released
  and closed, delivery ACKed. Next: validate the new head, focused
  confirmation review of the F5 boundary, then the owner merge decision.
- Second fix round delivered at `c7e5ab7` (shared redaction boundary at the
  push path with a deferred named-return scrub; `redactedError` wrapper
  preserves `errors.Is`/`errors.As` and the 502/400/404 statuses; regressions
  for pull/start/remove/agent-write/ping/revert/close-log token injections,
  wrapped-sentinel classification, and body-limit edges; report corrections).
  Exact-head push CI `36407929682` SUCCESS, checkout clean, no PR. F5
  confirmation review dispatched: Task `task_57581be50ab0`, Dispatch
  `ctx_0d7fdf5880ae`, verdict target `be-6.2-fix2-review.md`. Codex stayed
  pinned at 0.157.1 and the reviewer terminal was prepared before dispatch (no
  update prompt appeared).
- F5 confirmation review returned FIX-FIRST on one narrow residual (R1, P2):
  `push` still logged the raw convergence `reason` at WARN while the credential
  env was available (e.g. an image-drift reason containing a token); B2 and all
  listed returned-error paths were confirmed fixed. Third fix round dispatched
  to the retained worker session: Task `task_c373215c386e`, Dispatch
  `ctx_f0cf1a974961` (scrub the WARN reason, add a token-bearing image-drift
  log regression, narrow doc/report coverage claims). Reviewer terminal
  released and closed, delivery ACKed; verdict file `be-6.2-fix2-review.md`.
- Third fix round delivered at `2ec800b` (R1: the convergence WARN reason is
  scrubbed through the shared `redactEnvText` helper with the node's credential
  env, WARN level and drift context kept; token-bearing image-drift log
  regression; doc/report claims narrowed; third-round evidence tables).
  Exact-head push CI `36409367869` SUCCESS, checkout clean, no PR. Note: that
  dispatch's worker_done inbox message was capability-rejected on first send
  (`ctx_f0cf1a974961`), but `worker-show` reports the dispatch
  completed/succeeded at 10:23:42 - trust the dispatch state, not a single
  rejected inbox message. R1 confirmation review (Task `task_55aee36cdfc7`,
  Dispatch `ctx_9aa8a2ce6096`) returned **MERGE-GO** (`be-6.2-fix3-review.md`;
  no blocking findings in the delta, one non-blocking report typo recorded).
  Reviewer terminal closed, `opencode.json` deleted from the worktree, checkout
  clean. Coordinator created draft
  [PR #55](https://github.com/justindeelux/gotham/pull/55); PR CI `36409965615`
  and PR E2E `36409965614` both SUCCESS at exact head
  `2ec800b772be2a885a0576d2f55cc5d8bf4e580e`.
- **Merged 2026-09-28 (owner-authorized):** PR #55 marked ready and merged as
  `e0ec802f7962c7034a1c92b515394136dd2e489b`; local main fast-forwarded
  `3705260..e0ec802`, `go build ./...` exit 0. `worker-release` on
  `ctx_f0cf1a974961` recorded `retained` (`external_terminal`,
  `processAction: none`); no reclaimable terminals. Post-merge cleanup done:
  worktree removed, `feat/p6-ssl` deleted locally and on origin, and the
  worktree terminal `term_d60e7e5a` closed; `term_38daecc0` is stale.
- Caveat recorded from the worker: this workspace's GitNexus index resolves no
  Go symbols for `internal/proxy`, so `impact()` could not run on those
  symbols; blast radius was assessed by package imports plus the full suite
  instead. Re-check the index before the next proxy edit.

## Settled task 9: FE domains follow-up (merged `7ccda27`)

- Run: `run_4019148921ea`; Task: `task_990373d0ef60`; authoritative Dispatch:
  `ctx_1b7cb74667d5`; worker terminal: `term_17a709b6-61d5-426a-b1e7-df526330605e`
  ("P6 domains FE worker", OpenCode, variant `max` verified).
- Worktree: `/Users/ndtpro/orca/workspaces/gotham/p6-domains-followup`, branch
  `feat/p6-domains-followup` from main `15f3da9`. `web/node_modules` absent →
  worker runs `npm ci`.
- Scope: frontend-only. Extend the proxy API client/store with the BE-6.3
  redirects CRUD and certificate `status`/`not_after`; replace the FE-6.1
  "backend pending" stubs for redirects and certificate status/expiry with live
  data (`present` + expiry, `absent`, `unknown`; never fabricate); keep only the
  router list stubbed (BE-6.3 added no router-list API); rebuild + commit
  webdist; update the Playwright domains smoke. Mockup parity
  (`docs/design/domains.html`), English copy, Naive UI + tokens.
- Status: delivered at `f95abfb` (typed redirects CRUD + certificate
  `status`/`not_after` in the proxy client/store; live redirect-rules section on
  `/domains`; live Status/Expires rendering; only the Routers tab stays stubbed;
  rebuilt committed webdist; Playwright smoke covers the redirect flow +
  present/absent/unknown). The worker's first `worker_done` was capability-
  rejected (`dispatch_capability_invalid`) and it re-sent; `worker-list` is the
  authority and shows the dispatch `succeeded`/`completed` with `nextAction:
  none`, so the work is accepted (trust the dispatch state, not one rejected
  inbox message). Draft [PR #61](https://github.com/justindeelux/gotham/pull/61).
  Independent review Task `task_4ecb602dbf33` / Dispatch `ctx_920295c6dd9d`
  (Codex, read-only) in flight; then the owner merge decision.
- Review returned **MERGE-GO** (`fe-6.x-review.md`): delta clean of blocking
  correctness/API-contract/credential-boundary/build-dist issues; two LOW notes
  (DomainsPage preserve-path copy should mention the query string; the smoke
  could assert the Expires placeholder for absent/unknown). Reviewer terminal
  released + closed. PR #61 checks 10/10 SUCCESS at the exact head. Owner merge
  decision pending.
- **Merged 2026-09-28 (owner-approved):** PR #61 marked ready and merged as
  `7ccda27` (head `f95abfb`), 10/10 checks SUCCESS at the exact head; local main
  fast-forwarded `15f3da9..7ccda27`, `go build ./...` exit 0. Cleanup done:
  worker terminal `term_17a709b6` closed, `p6-domains-followup` worktree
  removed, `feat/p6-domains-followup` deleted locally and on origin. Two LOW
  review notes deferred to `docs/TODO.md` Phase 6 residuals. **Phase 6 complete.**
- **Note:** the coordinator terminal handle changed to
  `term_46c4374f-b620-4c9e-8a6c-bc95047cc431` (Orca reincarnated it); the old
  `term_c854340b` handle in earlier notes is stale. The spare-terminal trick is
  recorded in the launch recipe for parallel runs.

## Settled task 8: BE-6.3 domain redirects + certificate status/expiry (merged `15f3da9`)

- Run: `run_8444c5ec2579`; Task: `task_9123d5d5585b`; authoritative Dispatch:
  `ctx_38eae37d918c`; worker terminal: `term_3c7dae3b-4c9c-4639-94e9-01768a8f44b4`
  ("P6 redirects worker", OpenCode, variant max verified).
- Worktree: `/Users/ndtpro/orca/workspaces/gotham/p6-redirects-status`, branch
  `feat/p6-redirects-status` from main `7ed4232` (which carries the BE-6.3 plan
  section added by [PR #58](https://github.com/justindeelux/gotham/pull/58),
  merge `7ed4232`).
- Scope: forward-only migration + `internal/proxy` store/service/routes for
  domain→domain redirect rules, generator support for the redirect middleware
  (YAML+TOML goldens), and certificate status/expiry sourced truthfully from the
  node's Traefik `acme.json` via the agent (redacted, read-only; `unknown` when
  the node is unreachable). **Design gate:** the worker must `ask` before
  inventing the redirect scope/code and the ACME-storage read boundary/schema.
- Status: delivered at `c75c4c1` — migration `00016_domain_redirects`,
  `/v1/proxy/redirects` CRUD, terminating `redirectRegex` + `servers: []` noop
  service, additive `ReadACMEStorage` RPC (keys never returned/logged), and
  computed `status`(`absent|present|unknown`)+`not_after` on the certificate
  API. All four design-gate conditions are test-covered; gated e2e green
  (real 301 with preserved path, restart persistence, fixture `acme.json`),
  push CI `36440422795` SUCCESS. Draft
  [PR #60](https://github.com/justindeelux/gotham/pull/60). Independent review
  Task `task_1fc52cf28fe9` / Dispatch `ctx_efd20c6bdad7` (Codex, read-only) in
  flight; verdict target `be-6.3-review.md`.
- **PR #60 CI is RED (found by the coordinator):** the `phase 4 end-to-end`
  job fails at `internal/e2e/p6_redirects_test.go:293`
  (`status without storage = "unknown", want absent`) — the pre-fixture read is
  classified `unknown` instead of `absent`, so the read path treats a
  missing/absent ACME dir/file as an error in CI (works locally). Blocker for
  merge; a fix round will cover this plus any reviewer findings once the
  read-only review finishes (no concurrent edits while it reads).
- Independent review (Task `task_1fc52cf28fe9` / Dispatch `ctx_efd20c6bdad7`,
  Codex) returned **FIX-FIRST** (`be-6.3-review.md`): no key leak; proto
  additive + buf
  breaking/drift clean; migration forward-only; conditions 1/3/4 hold. Findings:
  HIGH host variants (`:port`, trailing dot) bypass the terminating redirect
  (condition 2); MEDIUM no-chain guard one-directional (create `a→b` then `b→c`
  passes) + cross-node caveat; MEDIUM wildcard `main` name not covered; LOW
  HEAD documented as 301/302 but Traefik sends 308/307. Reviewer terminal
  released + closed. Combined fix round (4 findings + the CI `absent`/`unknown`
  root cause) dispatched: Task `task_2b11d169f7cd` / Dispatch
  `ctx_551eb90e4a49`. Then focused re-review + green CI before the owner merge
  decision.
- Fix round delivered at `80c3e2a` (all 5 items: host-form termination
  live-verified against traefik:v3.7 + request-level asserts; two-directional
  no-chain guard on create/update/enable with the generator as the race net;
  wildcard `main` coverage; GET-only 301/302 vs HEAD 308/307 docs corrected;
  CI `absent` root cause fixed by the agent preparing `acme.json` itself).
  Full `GOTHAM_E2E=1` e2e green locally (96s); PR #60 checks 9/9 SUCCESS at
  `80c3e2a`. Focused re-review Task `task_43e6a04e81f2` / Dispatch
  `ctx_99cffa0a07c1` in flight; verdict target `be-6.3-fix-review.md`, then the
  owner merge decision.
- Fix re-review (Task `task_43e6a04e81f2` / Dispatch `ctx_99cffa0a07c1`) returned
  **FIX-FIRST** (narrow, `be-6.3-fix-review.md`): the two-directional guard,
  wildcard primary, HEAD docs, ACME prep, key hygiene, proto/migration/drift all
  pass and CI e2e is 9/9 green; remaining are (1) HIGH — a router-accepted
  bracketed authority `Host: [old.example.com]:80` still misses the regex and
  falls through (proved by an offline Go probe), and (2) MEDIUM — the docs must
  state the live cross-node no-chain convergence limitation honestly. Reviewer
  terminal released + closed. Fix round 2 (Task `task_6d18eaed04ba` / Dispatch
  `ctx_cd0f4c26ed14`) dispatched: fix the WHOLE authority class (prefer dropping
  the host from the regex since the router already matches it) + request-level
  regressions for bracketed/non-numeric/empty ports and trailing dot, plus the
  docs limitation. Then a final focused re-review and green CI before merge.
- Fix round 2 delivered at `672effa`: the redirect pattern drops host
  re-encoding entirely (the middleware is attached only to the matching
  `Host(source)` router), so every accepted authority terminates by
  construction — live-verified against `traefik:v3.7` with request-level
  regressions for bracketed-with-port, non-numeric/empty ports and trailing dot;
  the no-chain guarantee + live cross-node convergence limitation documented in
  plan/package/code/migration comments. Coordinator independently confirmed
  PR #60 checks 9/9 SUCCESS and E2E run `36447350258` + CI run `36447350307`
  success at the exact head. Final confirmation review Task `task_7108abc287d9`
  / Dispatch `ctx_5c2f8fd04bb7` in flight; verdict target
  `be-6.3-final-review.md`, then the owner merge decision.
- Final confirmation review (Task `task_7108abc287d9` / Dispatch
  `ctx_5c2f8fd04bb7`) confirmed the HIGH authority fix and all offline
  regressions pass, but returned **FIX-FIRST** for one MEDIUM leftover: two
  unconditional no-chain comments (`redirects.go:223`, `queries/proxy.sql:227`
  → generated `sqlc/proxy.sql.go`) contradict the honest plan/package docs.
  Reviewer terminal released + closed. Comment-only Fix round 3 (Task
  `task_17a578a17832` / Dispatch `ctx_cd8191947ed5`) dispatched: qualify the
  comments (sequential rejection vs racing writes persisting until nodes
  converge) and regenerate sqlc. Then confirm CI green and decide merge. No
  runtime change expected, so a full 4th review is not planned for a
  comment-only delta — the coordinator will verify the diff + drift + CI.
- Comment-only Fix round 3 delivered at `c596b89`: the three unconditional
  no-chain claims qualified (global two-directional committed-state guard
  rejects sequential conflicting writes across nodes; racing writes can persist
  until nodes converge). Coordinator independently confirmed the delta is
  comment-only (0 non-comment changed lines; sqlc regenerated, not hand-edited),
  PR #60 checks 9/9 SUCCESS and E2E run `36449492056` + CI run `36449492201`
  success at the exact head. BE-6.3 is complete pending the owner merge
  decision.
- **Merged 2026-09-28 (owner-approved):** PR #60 marked ready and merged as
  `15f3da9` (head `c596b89`), 9/9 checks SUCCESS at the exact head; local main
  fast-forwarded `4199cca..15f3da9`, `go build ./...` exit 0. Cleanup done:
  worker terminal `term_3c7dae3b` and the spare consumer terminal
  `term_6594e557` closed, `p6-redirects-status` worktree removed,
  `feat/p6-redirects-status` deleted locally and on origin. Phase 6 backend is
  complete; the remaining Phase 6 item is the FE follow-up (replace the FE-6.1
  stubs with the live redirect/status data).

## Settled task 7: FE-6.1 Domains & SSL UI (merged `4199cca`)

- Run: `run_beb741178b0e`; Task: `task_4fd555242505`; authoritative Dispatch:
  `ctx_009bb73b5c6b`; worker terminal: `term_ecca6312-3742-409b-8bd2-53c4e41c3076`
  ("P6 domains worker", OpenCode). Variant `max` set via `ctrl+t` and verified
  in the footer before dispatch.
- Worktree: `/Users/ndtpro/orca/workspaces/gotham/p6-domains-ui`, branch
  `feat/p6-domains-ui` from main `37741c3`. `web/node_modules` absent → worker
  runs `npm ci`.
- Scope: frontend-only. Activate the sidebar `domains` entry with a `/domains`
  page (DNS providers CRUD via `/v1/proxy/dns-providers`; certificate intents
  via `/v1/proxy/certificates`), a `DomainEditor` in `ApplicationDetailPage`
  (edit `base_domain` + that app's certificate config), mockup parity for
  `docs/design/domains.html` where the API supports it, English copy, Naive UI
  + existing dark tokens, rebuilt committed `internal/server/webdist`.
- **Known backend gaps (must be explicit stubs, never fabricated):** no
  domain→domain redirect backend (only HTTP→HTTPS redirect), and the certificate
  API exposes no issuance status or expiry. A follow-up backend package can add
  those if the owner wants them.
- Status: delivered at `cd74b8a` (typed proxy client/store, `/domains` page,
  `DomainEditor` in app detail, sidebar/router activation, rebuilt webdist,
  Playwright domains smoke, `ui-e2e.yml` `GOTHAM_SECRET_KEY` for the smoke's
  provider-write path). Draft [PR #59](https://github.com/justindeelux/gotham/pull/59);
  PR checks 10/10 SUCCESS at `cd74b8a`. Independent review Task
  `task_a9586e25de3c` / Dispatch `ctx_6a105792ece7` (Codex, read-only) returned
  **FIX-FIRST** (`fe-6.1-review.md`): API contracts, honest stubs, language,
  conventions, dist integrity and TODO status all pass; one MEDIUM (plaintext
  credential retained in page component state after submit/dismissal in
  `DomainsPage.vue`) and one LOW (e2e credential assertions use `getByText`,
  cannot catch an input-value leak). Reviewer terminal released + closed. Fix
  round Task `task_a980db69e372` / Dispatch `ctx_2d28675cda17` in flight; then a
  focused re-review, then the owner merge decision.
- Fix round delivered at `81eee06` (credential cleared after every write and on
  every dismissal path incl. after-leave; e2e asserts blank reopen + no proxy
  response/storage leak, proven by a revert bite-check). Focused re-review (Task
  `task_76bf9e303be5` / Dispatch `ctx_541293dabbdc`) returned **MERGE-GO**
  (`fe-6.1-fix-review.md`). **Merged 2026-09-28 (owner-approved):** PR #59
  marked ready and merged as `4199cca` (head `81eee06`), 10/10 checks SUCCESS at
  the exact head; local main fast-forwarded `7ed4232..4199cca`, `go build ./...`
  exit 0. Cleanup done: worker terminal `term_ecca6312` closed, `p6-domains-ui`
  worktree removed, `feat/p6-domains-ui` deleted locally and on origin.

## Settled task 6: BE-6.2 real DNS-01 issuance acceptance (merged `2788a4e`)

- Run: `run_6e61913b2b1b`; Task: `task_1e8b343b47a0`; authoritative Dispatch:
  `ctx_28d0b259010c`; worker terminal: `term_9343d1b5-23c0-4ab7-91bd-062f876a09bc`
  ("P6 issuance worker", OpenCode). Variant `max` set in the TUI by cycling
  `variant_cycle` (`ctrl+t`) until the footer read
  `Build auto · DeepSeek V4.1 Flash OpenCode Go · max`, then verified from the
  first session export (`variant=max`). Project config `opencode.json`
  (`model` only) loaded; note config `#variant` and `agent.build.variant` do NOT
  take effect in this build — the interactive cycle is the working method.
- Worktree: `/Users/ndtpro/orca/workspaces/gotham/p6-issuance`, branch
  `feat/p6-issuance` from main `e0ec802`.
- Owner-provided credential (never committed/logged): `~/.config/gotham/cf-test.env`
  (mode 600) with `CF_DNS_API_TOKEN`, `GOTHAM_TEST_DOMAIN=gotham.deelux.dev`,
  `GOTHAM_TEST_ZONE=deelux.dev`.
- Scope: add an ACME `caServer` knob (empty = today's production default),
  add a gated real DNS-01 E2E acceptance; iterate on Let's Encrypt staging then
  run production once; confirm the cert inside Traefik. Token hygiene and a
  leak scan are hard constraints.
- Status: authoritative `worker_done` `msg_ea65fb32921a` (subject "BE-6.2 real
  DNS-01 issuance: staging + one production PASS, pushed f43f0a9") verified
  completed/succeeded. Head `f43f0a92c8f10efea25d817da5af4470eb7c48b1` pushed
  to `origin/feat/p6-issuance`; checkout clean except untracked `opencode.json`.
  Push CI `36421296858` SUCCESS at the exact head. Deliverable: `ACMEConfig.CAServer`
  (empty = production default) + `Config.CAServer` plumbing, staging/empty
  goldens, gated `TestP6DNS01Issuance` (staging default, production opt-in),
  docs. Real issuance observed: staging chain
  `gotham.deelux.dev → (STAGING) Dastardly Durum YR1 → (STAGING) Yonder Yam Root YR`
  and production chain `gotham.deelux.dev → YR2 → Root YR → ISRG Root X1`;
  challenge TXT created/cleaned via the Cloudflare API; cert present in
  `acme.json`. Coordinator independently confirmed the chains (openssl on the
  evidence PEMs), the leak scan (0 hits: tree, untracked, evidence/report, full
  git history) and the exact-head push CI. Report:
  `be-6.2-issuance-report.md`; evidence: `evidence-be-6.2-issuance/`. Worker
  terminal `term_9343d1b5` retained for a possible fix round; delivery ACKed;
  no reclaimable terminals. Draft [PR #56](https://github.com/justindeelux/gotham/pull/56)
  created; merge/review is owner-gated. Owner chose independent review first
  (cleanup after merge). Reviewer Task `task_5c83401b4c80`, Dispatch
  `ctx_b9b56455c0f8` on Codex `gpt-6-sol` (v0.157.1 pinned, update banner
  dismissed; read-only delta scope) returned **FIX-FIRST**
  (`be-6.2-issuance-review.md`): product `caServer` change confirmed correct,
  real evidence consistent, secrets clean; five findings all in
  `internal/e2e/p6_issuance_test.go` — F1 HIGH TXT ownership ("absent from
  baseline" ≠ owned), F2 MEDIUM baseline taken after SyncServer starts Traefik,
  F3 MEDIUM failure-path resource leaks, F4 MEDIUM issuer strings do not
  cryptographically verify a CA chain, F5 MEDIUM cleanup failure stays green.
  Reviewer terminal released then closed. Fix round dispatched to the retained
  worker session: Task `task_2fffae98c396`, Dispatch `ctx_31ef41d598df`
  (re-run staging + ONE more production issuance to prove the strengthened
  checks). Delivered at `72b7545` (staging 70.3s, second/final production
  56.6s; ownership proven by recomputed challenge value, x509.Verify chains,
  acme.json leaf DER compare, cleanup failures fail, redacted failure logs).
- **CI-blocking defect found by the coordinator after fix round 1:** PR #56's
  `phase 4 end-to-end (GOTHAM_E2E=1)` check is RED because
  `TestP6DNS01Issuance` Fatalfs when the credential env is absent and the repo
  CI sets `GOTHAM_E2E=1` without it (run `36421606162` at `f43f0a9`,
  `p6_issuance_test.go:422`). The review missed it. Fix round 2 dispatched:
  Task `task_2324f7cabf86`, Dispatch `ctx_da4c8fb52c6a` — require an explicit
  `GOTHAM_E2E_DNS01=1` opt-in so CI skips (GOTHAM_E2E=1-only), fatal only when
  explicitly opted in; re-run staging only (no third production issuance); the
  CI-equivalent skip command is mandatory evidence. Fix round 2 delivered at
  `484a683`: live DNS-01 now requires `GOTHAM_E2E_DNS01=1` in addition to
  `GOTHAM_E2E=1`; CI-only run SKIPs (coordinator independently reproduced:
  `GOTHAM_E2E=1` without credentials/flag → `--- SKIP: TestP6DNS01Issuance`,
  PASS, exit 0), explicit opt-in without credentials still fails, staging
  re-passed (59.2s), no third production issuance. PR #56 checks all green
  except the phase-4 E2E, which was pending/confirming at this checkpoint.
  Next: focused re-review of the fix delta (`f43f0a9..484a683`), then the owner
  merge decision. Not claimed: wildcard live issuance (goldens only).
- Re-review of `f43f0a9..484a683` (Codex, Task `task_24f171c1b68a` / Dispatch
  `ctx_32d23eda3171`) returned **FIX-FIRST** again but narrower
  (`be-6.2-issuance-fix-review.md`): challenge ownership, crypto verification,
  CI gate and DB/proxy cleanup CONFIRMED resolved; three MEDIUM cleanup gaps
  remained (test-only): preflight delete lacked current-value proof,
  `p6RunNginx` leaked the backend on start failure, and its removal errors only
  logged. Reviewer terminal released + closed.
- Fix round 3 (Task `task_1851513e5b78` / Dispatch `ctx_8044428e5066`) delivered
  at `d43fb91` (test-only + docs): preflight deletion now proves current value,
  `p6RunNginx` registers cleanup for any returned container ID before handling a
  start error and fails on removal error, evidence regenerated; coordinator
  independently reproduced the CI-equivalent SKIP; worker ran the older
  `TestP6ProxySyncProduction` (37.82s pass) and one staging issuance (66.72s).
  Final confirmation review (Task `task_0e37563666d8` / Dispatch
  `ctx_5ca054af87f0`, Codex read-only) returned **MERGE-GO**
  (`be-6.2-issuance-final-review.md`): delta clean, no HIGH/MEDIUM/LOW findings,
  prior safeguards intact. Reviewer terminal released + closed.
- **Merged 2026-09-28 (owner-authorized after MERGE-GO):** PR #56 marked ready
  and merged as `2788a4e45dc3eb0685fcc78f9b442a562f9a153b` (head `d43fb91`);
  9/9 PR checks SUCCESS at the exact head; local main fast-forwarded
  `e0ec802..2788a4e`, `go build ./...` exit 0. Cleanup done (owner-approved):
  worker terminal `term_9343d1b5` closed, `p6-issuance` worktree removed (the
  untracked `opencode.json` pin had to be deleted first), `feat/p6-issuance`
  branch deleted locally and on origin; no `p6-*`/`gotham-traefik` containers
  remain; only the coordinator terminal is live. Owner should now
  rotate/delete the Cloudflare test token.
  Not claimed: wildcard live issuance (goldens only).

## Merged (owner decision 2026-09-28: merge now, L1/L2 to residuals)

PR #51 marked ready and merged as `89bd579` (`Merge pull request #51`,
head `ef29769`). Local main fast-forward pulled; uncommitted handoff files
preserved; `go build ./...` exit 0 on merged main. BE-6.1 is accepted.
Next: BE-6.2 SSL and FE-6.1 domains UI, each in its own work package/PR, only
on owner instruction. Phase gate applies before Phase 7+.

## Next decision: Phase 9 in progress (owner-extended autonomy)

**Phase 8 is complete** (all five packages merged; see the settled tasks). The owner
authorized **Phase 9 (Self-update & Release)** on 2026-09-30 with the same standing
autonomy and set the execution model effort to **`high`**. Phase 9 =
`docs/plan/10-self-update-release.md`: **BE-9.1 control-plane self-update (active)**,
BE-9.2 agent remote update, INFRA-9.1 release pipeline, then the mandatory **gate G2**
(`code-reviewer` + `security-reviewer` over signatures, distribution channel and
runtime privileges). M9 exit criteria: signed `gotham update` with rollback, remote
agent self-update via the CP, and GoReleaser + a one-line install script.

## Worker routing and environment

- Real Orca implementation worker: OpenCode, `opencode-go/deepseek-v4.1-flash`;
  hard/security/state-machine work uses variant `max`. Independent reviewer:
  Codex `gpt-6-sol`. No silent model fallback or global defaults change.
- Orca 1.4.215 rejects OpenCode `worker-start --model`. This run verified the
  model through the OpenCode session API (`session export` shows
  providerID/model/variant) and attached the proven terminal via
  `worker-start --terminal`. A fresh `--agent opencode` terminal defaulted to the
  wrong model and was abandoned before any turn; an input-accepted receipt alone
  is not observed model execution.
- Owner rule for terminal reuse: when reusing an existing terminal for a new
  task, run `/compact` first if the new task is only loosely related to the old
  session, or start a fresh session if it is unrelated. Prefer a dedicated
  worktree + fresh terminal per work package over reusing another package's
  terminal (owner flagged P5-on-P6-terminal mixing; fixed by abandon + clean
  relaunch with zero lost edits).
- Fresh OpenCode TUI terminals do NOT auto-start on dispatch input
  (input_accepted but idle prompt, no turn, no session). Procedure that works:
  pin the model with worktree-root `opencode.json` (`model` key,
  project config overrides global), submit the full preamble + spec via
  `terminal send --text ... --enter`, then re-attach with a new Task +
  `worker-start --terminal` once the session is live so the capability binds
  (first manual attempt's heartbeats were rejected with
  dispatch_capability_invalid). Never commit the pin file.
- Coordinator binding flaps between runs when background waiters get replaced;
  use `run-use --id <run>` to rebind before `check --ack`. A fenced background
  `check --wait` loses its delivery; re-read the inbox after rebinding.
- Codex CLI (updated 2026-09-30): the global `@openai/codex` was upgraded
  **0.157.1 → 0.159.1** because the owner asked for `gpt-6.1-sol` on review tasks:
  0.157.1 rejects it (`The 'gpt-6.1-sol' model is not supported when using Codex
  with a ChatGPT account`) while 0.159.1 runs it (verified: `codex exec -m
  gpt-6.1-sol` → `SOL-OK`; control `gpt-6-sol` works on both). **Reviewer terminals
  launch `codex -m gpt-6.1-sol`**; the account default in `~/.codex/config.toml`
  stays `gpt-6-sol`. Orca 1.4.215's `--for tui-idle` readiness check does **not**
  recognize 0.159.1 (timeout, same as 0.158.0) but the TUI is live: create the
  terminal, let `tui-idle` time out (or skip the wait), dismiss any banner with ESC,
  **submit the full preamble + spec manually** via `terminal send --text ... --enter`,
  then create the Task and `worker-start --terminal` once the session is live so the
  capability binds (the documented manual-dispatch recipe). `~/.codex/version.json`
  is stale (says latest 0.158.0); keep dismissing update prompts. Account note
  2026-09-30: the TUI warned **<25% weekly limit left** — reviewers burn quota, so
  batch review asks.
- Operator-created terminals are retained, never released or closed implicitly.
  A new Task/Dispatch may reuse a verified idle terminal after an explicit
  ownership decision. Capability tokens are private and revoked at settlement;
  never copy/reconstruct them from prior preambles or include them in reports.
- Local default Go was 1.27.1, incompatible with the installed linter build.
  Prior workers used `/usr/local/go/bin/go` (1.25.7) for lint with matching GOROOT;
  CI uses Go 1.22. Verify installed versions rather than assuming shell defaults.
- Docker Desktop needs `host.docker.internal` for the local ephemeral-port E2E;
  Linux CI exercises production `172.17.0.1`. Do not hard-code the local override
  into production. macOS test roots canonicalize `/var` to `/private/var`.
- Use disposable PostgreSQL and explicit `GOTHAM_TEST_DSN` for the verbose
  uniqueness/migration/history tests. E2E uses `GOTHAM_E2E=1` and requires free
  ports 80/443/8080. Refuse a pre-existing `gotham-traefik`, and delete only IDs
  the run owns. The shared `gotham` SSH test host was not modified by these packages.

## Execution gates

- Read `AGENTS.md`, `docs/process.md`, `docs/TODO.md` and `docs/plan/07-proxy-domains.md`.
- Use real Orca workers and the version-matched guide; verified CLI: `orca` 1.4.215.
- Workers own source/review; coordinator owns routing, supervision and checkpoint.
- GitNexus impact before symbol edits; warn HIGH/CRITICAL; detect changes before
  commits. Report stale/ambiguous graph limits honestly.
- Require build, tests, vet, lint, web build/type-check, generated-file consistency,
  package boundaries and actual task-specific acceptance execution.
- Process complete mailbox batches and authoritative settlement before ACK.
  Heartbeats/timeouts do not prove completion/exit. Release or explicitly retain
  settled workers; preserve operator-owned terminals.
- Disposable owned Docker only; no shared-service replacement, unrelated prune,
  deployment, or later phase without owner authorization.

## Dependencies and blockers

- BE-6.1 (merged #51/#53), BE-6.2 SSL (#55) and the real DNS-01 issuance
  acceptance (#56) are all merged and cleaned up. The only remaining Phase 6
  item is FE-6.1 (accepted API; compare matching design mockups, retain Naive
  UI, shared tokens, English copy and committed webdist), on owner instruction.
- **Real DNS-01 certificate issuance: VERIFIED and merged** (`2788a4e`, PR #56)
  against the owner's real Cloudflare zone `deelux.dev` / host
  `gotham.deelux.dev` — Let's Encrypt staging and one production issuance, cert
  served by Traefik and chain-verified. Live wildcard issuance and live HTTP-01
  issuance were NOT run (wildcard shape is covered by generator goldens). Phase 6
  remains open only for FE-6.1 (domains UI).
- P5 validation/fixes are reviewed and merged in
  [PR #52](https://github.com/justindeelux/gotham/pull/52), merge `718d19b`.
  Do not repeat this package; deferred LOWs remain in `TODO.md`.
