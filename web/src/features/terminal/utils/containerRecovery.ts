import type { ApplicationTerminalTarget } from "@/features/terminal/schemas/terminal";

/**
 * Shared container-target recovery rules (JUS-247).
 *
 * Both the application tab and the terminal store's window fallback rediscover
 * the running set the same way. Keeping the exact/only/selector decision in
 * one pure module stops the two paths from drifting: a replaced id is never
 * re-POSTed, one runnable target is unambiguous, and several always need an
 * explicit choice.
 */

/** ContainerTargetIdentity is the pair the control plane re-validates. */
export interface ContainerTargetIdentity {
  service: string;
  containerId: string;
}

/** containerTargetKey is the stable local key for one target identity. */
export function containerTargetKey(identity: ContainerTargetIdentity): string {
  return `${identity.service}\u0000${identity.containerId}`;
}

/** isSameContainerTarget compares a fresh row with a stored identity. */
export function isSameContainerTarget(
  target: ApplicationTerminalTarget,
  identity: ContainerTargetIdentity,
): boolean {
  return (
    target.service === identity.service &&
    target.container_id === identity.containerId
  );
}

/** containerTabTitle is the shared "application · service · container" label. */
export function containerTabTitle(
  applicationName: string,
  targetLabel: string,
): string {
  return [applicationName, targetLabel]
    .filter((part) => part !== "")
    .join(" · ");
}

/** RecoveryChoice is the deterministic next step after one discovery read. */
export interface RecoveryChoice {
  /** target is the exact or unambiguous container to open, if any. */
  target: ApplicationTerminalTarget | null;
  /** needsSelection is true when several containers remain and none is exact. */
  needsSelection: boolean;
}

/**
 * chooseRecoveryTarget resolves fresh targets. `preferExact` keeps a still
 * valid original target (the window fallback); the application restart path
 * passes false so a multi-target app always shows the explicit selector.
 */
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
