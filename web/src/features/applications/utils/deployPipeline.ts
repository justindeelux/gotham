import type { Deployment, DeploymentState } from "@/features/applications/api/applications";

/**
 * PIPELINE_ORDER is the canonical state walk from `internal/deploy/state.go`:
 * queued → cloning → building → pushing → starting → running. A rollback skips
 * cloning and building because it redeploys an already-pushed image.
 */
export const PIPELINE_ORDER: DeploymentState[] = [
  "queued",
  "cloning",
  "building",
  "pushing",
  "starting",
  "running",
];

/** One rendered node of the deploy pipeline. */
export interface PipelineStep {
  name: DeploymentState;
  mood: "is-done" | "is-active" | "is-failed" | "";
}

/**
 * pipelineStepsFor maps a deployment onto the rendered pipeline nodes.
 *
 * A failed deployment's row does not record which stage failed, so no stage
 * can be reported as completed: every stage is rendered as pending and a
 * terminal `failed` node carries the failure. The previous implementation
 * treated `failed` as if it were the last step, which marked every unexecuted
 * stage as done.
 */
export function pipelineStepsFor(current: Deployment | null): PipelineStep[] {
  if (!current) {
    return [];
  }
  const order =
    current.kind === "rollback"
      ? PIPELINE_ORDER.filter((name) => name !== "cloning" && name !== "building")
      : PIPELINE_ORDER;

  if (current.state === "failed") {
    return [
      ...order.map((name) => ({ name, mood: "" as const })),
      { name: "failed", mood: "is-failed" as const },
    ];
  }

  if (current.state === "running") {
    return order.map((name) => ({ name, mood: "is-done" as const }));
  }

  const currentIndex = order.indexOf(current.state);
  return order.map((name, index) => {
    if (index < currentIndex) {
      return { name, mood: "is-done" as const };
    }
    if (index === currentIndex) {
      return { name, mood: "is-active" as const };
    }
    return { name, mood: "" as const };
  });
}
