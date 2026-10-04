import { useMessage } from "naive-ui";
import { ref, watch, type Ref } from "vue";

import { describeApplicationError, updateApplication } from "@/features/applications/api/applications";
import type { Application } from "@/features/applications/api/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";

/** HOST_PATTERN mirrors the generator's ValidateDomain boundary. */
const HOST_PATTERN =
  /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/;

/** isValidDomain accepts an empty value (clears) or a plain hostname. */
export function isValidDomain(value: string): boolean {
  if (value === "") {
    return true;
  }
  return value.length <= 253 && HOST_PATTERN.test(value);
}

/**
 * Base-domain editing behind the Domains tab. The domain is written through
 * the applications API; the certificate panel records it at save time.
 */
export function useApplicationDomain(application: Ref<Application>) {
  const message = useMessage();
  const appsStore = useApplicationsStore();

  const baseDomain = ref<string>(application.value.base_domain);
  const savingDomain = ref(false);
  const domainError = ref<string | null>(null);

  /** handleSaveDomain writes base_domain through the applications API. */
  async function handleSaveDomain(): Promise<void> {
    const next = baseDomain.value.trim().toLowerCase();
    if (!isValidDomain(next)) {
      domainError.value =
        "Enter a plain hostname such as app.example.com (letters, digits, hyphens and dots; no wildcard).";
      return;
    }
    domainError.value = null;
    savingDomain.value = true;
    try {
      await updateApplication(application.value.id, { base_domain: next });
      baseDomain.value = next;
      await appsStore.fetchApplication(application.value.id);
      message.success("Application domain saved.");
    } catch (error) {
      domainError.value = describeApplicationError(error);
    } finally {
      savingDomain.value = false;
    }
  }

  watch(
    () => application.value.base_domain,
    (value) => {
      baseDomain.value = value;
    },
  );

  return { baseDomain, savingDomain, domainError, handleSaveDomain };
}
