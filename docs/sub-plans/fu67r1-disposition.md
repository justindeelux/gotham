# JUS-67 fix round 1 — disposition (PR #214, head e64fec8a)

Branch `feat/fu67-git-host-denylist` rebased onto origin/main (0c950fc8, JUS-68)
and force-pushed; PR stays draft. No web/ changes, no migrations, webdist untouched.

## Per-finding disposition

1. HIGH redirect bypass — FIXED. `pinGitRemoteHost` now appends
   `http.followRedirects=false` (deduped against askpassEnv) for every
   http(s) git invocation: clone, ls-remote, private probes. Regression
   tests `TestGitCloneRefusesRedirect`, `TestGitLsRemoteRefusesRedirect`,
   `TestGitProbeRefusesRedirect` run real git against a two-server httptest
   oracle and assert the target gets 0 hits, the error quotes the 302, and
   no "redirecting to" line leaks. Mutation check: all four (incl. the
   `TestGitClonePinsPublicHost` env assertion) fail with the append disabled.
2. MEDIUM/LOW mixed A records — FIXED (test added, code already correct).
   `TestGitMixedARecordsRefused`: `[203.0.113.10, 10.0.0.5]` refuses at pin
   and in full Clone without running git.
3. LOW validateCloneURL nil-on-parse-failure — FIXED. Unparseable URLs now
   refuse at creation (`unsupported clone URL`, ErrValidation).
4. LOW flag parsing inconsistency — FIXED. Single `parseGitAllowPrivateHosts`
   (strconv.ParseBool, exactly matching viper/cast string semantics; YAML
   ints go through cast nonzero→true on both sides). Tests: truthy/falsy
   table + YAML `1` load test.
5. LOW residuals + git floor — FIXED. docs/install.md documents ssh/git
   unpinned, proxy bypass, git>=2.37 requirement, localhost-name note.
   `checkGitVersionForPin` (sync.OnceValue-cached `git --version`, parsed by
   tested `parseGitVersion`) warns at service construction when git < 2.37.
6. Nits — ALL FIXED. Dup probeLookup comment removed; `64:ff9b:1::/48` and
   `::/96` (IPv4-compatible) added to always-deny with table rows (the ::1
   allow-private row caught the ::/96 shadowing, fixed by checking loopback
   first); every denial is the single generic `clone URL host is not
   allowed` (literal and resolution paths assert byte-identical text, so the
   error is no resolution oracle).

## Command outputs (local)

- go build ./... PASS; go vet ./... PASS; golangci-lint run 0 issues
- go test ./... all packages PASS (deploy + config re-ran -count=1)
- web/: npm run lint PASS, type-check PASS, build PASS, npm test 93 passed;
  git status shows no webdist drift (no web/ sources changed)
- GOTHAM_E2E=1 suite: all PASS except TestP5BackupS3TargetMinIO, which fails
  identically on clean main (MinIO fixture never becomes ready here)

## CI (PR #214, head e64fec8a): 8/8 PASS

fmt+vet+test+build, lint, DB-backed, phase-4 e2e (5m37s), web build+drift,
proto, sqlc, installer scripts.
