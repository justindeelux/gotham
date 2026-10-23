import type { Deployment, DeploymentState } from "@/features/applications/api/applications";

export const PIPELINE_ORDER: DeploymentState[] = [
  "queued",
  "cloning",
  "building",
  "pushing",
  "starting",
  "running",
];

export interface PipelineStep {
  name: DeploymentState;
  mood: "is-done" | "is-active" | "is-failed" | "";
}

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
