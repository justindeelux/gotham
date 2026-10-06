import { useMessage } from "naive-ui";
import { onUnmounted, ref, watch, type Ref } from "vue";

import { describeApplicationError, updateApplication } from "@/features/applications/api/applications";
import type { Application } from "@/features/applications/api/applications";
import { hostDomainSchema } from "@/features/applications/schemas/applications";
import { activeLocale, i18n, onLocaleChange } from "@/shared/i18n";
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
  /**
   * domainServerError keeps the failure behind a server-side banner so the
   * locale refresh below can re-derive it; validation feedback re-resolves
   * from the typed value instead.
   */
  const domainServerError = ref<unknown>(null);

  /**
   * tr resolves one applications message in the current locale. Reading
   * activeLocale pins the caller to the language switch.
   */
  function tr(key: string): string {
    void activeLocale.value;
    return String(i18n.global.t(key));
  }

  // An already-visible domain error re-resolves in the new locale on switch:
  // the typed value stays, no submit fires, untouched fields stay clean.
  // Validation feedback re-runs against the draft; a retained server failure
  // re-derives from its raw error.
  const stopLocaleRefresh = onLocaleChange(() => {
    if (domainError.value === null) {
      return;
    }
    if (domainServerError.value !== null) {
      domainError.value = describeApplicationError(domainServerError.value);
      return;
    }
    const issues = fieldErrors(hostDomainSchema, baseDomain.value.trim().toLowerCase());
    domainError.value = issues[0] ?? null;
  });
  onUnmounted(stopLocaleRefresh);

  /** handleSaveDomain writes base_domain through the applications API. */
  async function handleSaveDomain(): Promise<void> {
    const next = baseDomain.value.trim().toLowerCase();
    const issues = fieldErrors(hostDomainSchema, next);
    if (issues.length > 0) {
      domainServerError.value = null;
      domainError.value = issues[0];
      return;
    }
    domainError.value = null;
    domainServerError.value = null;
    savingDomain.value = true;
    try {
      await updateApplication(application.value.id, { base_domain: next });
      baseDomain.value = next;
      await appsStore.fetchApplication(application.value.id);
      message.success(tr("applications.detail.domainSaved"));
    } catch (error) {
      domainServerError.value = error;
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
