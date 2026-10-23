import type { ApplicationTerminalTarget } from "@/features/terminal/schemas/terminal";

export interface ContainerTargetIdentity {
  service: string;
  containerId: string;
}

export function containerTargetKey(identity: ContainerTargetIdentity): string {
  return `${identity.service}\u0000${identity.containerId}`;
}

export function isSameContainerTarget(
  target: ApplicationTerminalTarget,
  identity: ContainerTargetIdentity,
): boolean {
  return (
    target.service === identity.service &&
    target.container_id === identity.containerId
  );
}

export function containerTabTitle(
  applicationName: string,
  targetLabel: string,
): string {
  return [applicationName, targetLabel]
    .filter((part) => part !== "")
    .join(" · ");
}

export interface RecoveryChoice {
  target: ApplicationTerminalTarget | null;
  needsSelection: boolean;
}

export function chooseRecoveryTarget(
  targets: ApplicationTerminalTarget[],
  preferred: ContainerTargetIdentity | null,
  preferExact: boolean,
): RecoveryChoice {
  if (preferExact && preferred !== null) {
    const exact = targets.find((target) =>
      isSameContainerTarget(target, preferred),
    );
    if (exact !== undefined) {
      return { target: exact, needsSelection: false };
    }
  }
  if (targets.length === 1) {
    return { target: targets[0]!, needsSelection: false };
  }
  return { target: null, needsSelection: targets.length > 1 };
}
