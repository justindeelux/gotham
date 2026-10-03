import { ref } from "vue";
import type { Ref } from "vue";

import { createRequestGeneration } from "../utils/requestGeneration";

/**
 * In-flight guard for a wizard's create/validate requests.
 *
 * `begin()` captures a token before an await; `isCurrent(token)` reports
 * whether the wizard still owns that request. `reset()` invalidates every
 * in-flight request *and* clears the loading flags — without the latter a
 * close during Create/Validate leaves the reopened wizard with a permanently
 * loading button (the guarded `finally` blocks intentionally skip a stale
 * request's cleanup).
 */
export interface InFlightGuard {
  creating: Ref<boolean>;
  validating: Ref<boolean>;
  begin(): number;
  isCurrent(_token: number): boolean;
  reset(): void;
}

/** useInFlightGuard returns a fresh guard for one wizard instance. */
export function useInFlightGuard(): InFlightGuard {
  const generation = createRequestGeneration();
  const creating = ref(false);
  const validating = ref(false);
  return {
    creating,
    validating,
    begin: () => generation.current(),
    isCurrent: (token) => generation.isCurrent(token),
    reset: () => {
      generation.bump();
      creating.value = false;
      validating.value = false;
    },
  };
}
