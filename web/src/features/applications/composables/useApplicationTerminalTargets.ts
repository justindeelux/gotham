import { onScopeDispose, ref } from "vue";
import type { Ref } from "vue";

import { applicationTerminalFailure } from "@/features/terminal/api/terminal";
import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import { listApplicationTerminalTargets } from "@/features/terminal/api/terminal";
import type { ApplicationTerminalTarget } from "@/features/terminal/schemas/terminal";

/**
 * Lazy, single-flight discovery of an application's running containers
 * (JUS-247). It owns only the GET and its loading/error state; the session
 * composable decides whether one target auto-opens, a selector is shown, or
 * the empty/error state renders. `attempted` makes discovery explicitly lazy:
 * a bounded failure never retries itself.
 */
export interface ApplicationTerminalTargetsOptions {
  applicationId: () => string;
  /** enabled mirrors the known, write-level access gate. */
  enabled: () => boolean;
}

/** TargetsLoadResult is the outcome of one discovery read. */
export type TargetsLoadResult =
  | {
      ok: true;
      targets: ApplicationTerminalTarget[];
      serverId: string;
    }
  | { ok: false; error: TerminalErrorCode }
  | null;

export interface ApplicationTerminalTargetsState {
  targets: Ref<ApplicationTerminalTarget[]>;
  serverId: Ref<string>;
  loading: Ref<boolean>;
  loaded: Ref<boolean>;
  error: Ref<TerminalErrorCode | null>;
  attempted: Ref<boolean>;
  /** load performs one GET; concurrent callers share the same request. */
  load: () => Promise<TargetsLoadResult>;
  /** reset aborts an in-flight read and forgets the previous application. */
  reset: () => void;
  /** markAttempted records that the current application already discovered. */
  markAttempted: () => void;
}

export function useApplicationTerminalTargets(
  options: ApplicationTerminalTargetsOptions,
): ApplicationTerminalTargetsState {
  const targets = ref<ApplicationTerminalTarget[]>([]);
  const serverId = ref("");
  const loading = ref(false);
  const loaded = ref(false);
  const error = ref<TerminalErrorCode | null>(null);
  const attempted = ref(false);

  let controller: AbortController | null = null;
  let pending: Promise<TargetsLoadResult> | null = null;
  let token = 0;

  /** load reads the targets once; flapping joins the in-flight request. */
  function load(): Promise<TargetsLoadResult> {
    if (pending) {
      return pending;
    }
    const applicationId = options.applicationId();
    if (applicationId === "" || !options.enabled()) {
      return Promise.resolve(null);
    }
    controller?.abort();
    controller = new AbortController();
    const current = ++token;
    loading.value = true;
    error.value = null;
    pending = (async (): Promise<TargetsLoadResult> => {
      try {
        const resolved = await listApplicationTerminalTargets(
          applicationId,
          controller?.signal,
        );
        if (
          current !== token ||
          options.applicationId() !== applicationId
        ) {
          return null;
        }
        attempted.value = true;
        targets.value = resolved.containers;
        serverId.value = resolved.server_id;
        loaded.value = true;
        return {
          ok: true,
          targets: resolved.containers,
          serverId: resolved.server_id,
        };
      } catch (failure) {
        if (current !== token) {
          return null;
        }
        attempted.value = true;
        targets.value = [];
        loaded.value = false;
        error.value = applicationTerminalFailure(failure).code;
        return { ok: false, error: error.value };
      } finally {
        if (current === token) {
          loading.value = false;
        }
        pending = null;
      }
    })();
    return pending;
  }

  /** reset aborts and clears every target field for a new application. */
  function reset(): void {
    controller?.abort();
    controller = null;
    token += 1;
    pending = null;
    targets.value = [];
    serverId.value = "";
    loading.value = false;
    loaded.value = false;
    error.value = null;
    attempted.value = false;
  }

  /** markAttempted blocks an automatic rediscovery after a bounded failure. */
  function markAttempted(): void {
    attempted.value = true;
  }

  onScopeDispose(() => {
    controller?.abort();
    controller = null;
  });

  return {
    targets,
    serverId,
    loading,
    loaded,
    error,
    attempted,
    load,
    reset,
    markAttempted,
  };
}
