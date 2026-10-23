import { ref } from "vue";
import type { Ref } from "vue";

import { createRequestGeneration } from "@/shared/utils/requestGeneration";

export interface InFlightGuard {
  creating: Ref<boolean>;
  validating: Ref<boolean>;
  begin(): number;
  isCurrent(_token: number): boolean;
  reset(): void;
}

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
