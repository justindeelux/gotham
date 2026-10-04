import { useMessage } from "naive-ui";
import { ref, watch, type Ref } from "vue";

import { describeApplicationError, updateApplication } from "@/features/applications/api/applications";
import type { Application } from "@/features/applications/api/applications";
import { hostDomainSchema } from "@/features/applications/schemas/applications";
import { fieldErrors } from "@/shared/validation/naiveAdapter";
import { useApplicationsStore } from "@/features/applications/stores/applications";

/**
 * isValidDomain accepts an empty value (clears) or a plain hostname.
 * Schema-backed: see `schemas/applications.ts hostDomainSchema`.
 */
export function isValidDomain(value: string): boolean {
  return hostDomainSchema.safeParse(value).success;
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
    const issues = fieldErrors(hostDomainSchema, next);
    if (issues.length > 0) {
      domainError.value = issues[0];
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
