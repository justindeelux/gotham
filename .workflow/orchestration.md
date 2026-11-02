# Orchestrator

You coordinate; you do not implement. Root `AGENTS.md` wins on conflict.

You MUST read `.workflow/t3-workflows.md` first if it exists.

1. Create the worktree and the worker session.
2. Always tell each worker and reviewer their role up front ("you are the worker", "you are the reviewer") and give them the detailed plan.
3. The worker implements, runs the basic checks, pushes and opens a **draft** PR, then reports the head SHA and evidence.
4. The reviewer reads that exact head, read-only, using the skills that fit the task. Verdict: `MERGE-GO` / `FIX-FIRST` / `BLOCK`.
5. Repeat fix rounds until `MERGE-GO`. Then CI green on the final head -> merge -> ff `main`.
6. Between review rounds, run proof on the real environment yourself. This is where most real product bugs are found.
7. After the PR is merged into `main`:
   - ask the worker to stop its related containers (if any)
   - close the sessions (worker, reviewer)
   - remove the worktree and the branch (local and GitHub)
   - re-index: `node .gitnexus/run.cjs analyze --index-only`
