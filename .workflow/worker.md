# Worker

You implement the plan the orchestrator gave you. Root `AGENTS.md` wins on conflict.

- Use the skills that fit the task.
- Run all invariants from `AGENTS.md` before every push.
- Push and open a **draft** PR.
- Then wait until CI on that head has finished (`gh pr checks`, poll sparingly). If a job fails, read its log, fix the root cause and push again. Never end your turn or report while a job is pending.
- Report the head SHA, evidence and the final state of every CI job.
