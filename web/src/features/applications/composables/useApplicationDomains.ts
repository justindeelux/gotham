import { useMessage } from "naive-ui";
import { onMounted, onUnmounted, ref, watch, type Ref } from "vue";

import {
  addDomain,
  describeApplicationError,
  listDomains,
  removeDomain,
  setPrimaryDomain,
  type Application,
  type ApplicationDomain,
} from "@/features/applications/api/applications";
import { hostDomainSchema } from "@/features/applications/schemas/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import {
  bumpDomainsRefresh,
  useDomainsRefresh,
} from "@/features/applications/composables/domainRefresh";
import { describeProxyError, useProxyStore } from "@/features/domains";
import { activeLocale, i18n, onLocaleChange } from "@/shared/i18n";
import { fieldErrors } from "@/shared/validation/naiveAdapter";

/**
 * Alias-domain management behind the Domains tab (JUS-89): the primary domain
 * stays on the base-domain editor, while this composable lists the attached
 * hostnames and adds, removes or promotes them. Every mutation refetches the
 * application so the mirrored base_domain follows the rows.
 */
export function useApplicationDomains(application: Ref<Application>) {
  const message = useMessage();
  const appsStore = useApplicationsStore();
  const proxyStore = useProxyStore();
  /**
   * refreshTick is captured once during setup: inject() has no active
   * component instance after an await, so the async handlers below close
   * over this instead of calling inject() themselves.
   */
  const refreshTick = useDomainsRefresh();

  const domains = ref<ApplicationDomain[]>([]);
  const loadingDomains = ref(false);
  const domainsError = ref<string | null>(null);
  const newDomain = ref("");
  const addingDomain = ref(false);
  const addError = ref<string | null>(null);
  const busyDomainId = ref<string | null>(null);
  const rowErrorId = ref<string | null>(null);
  const rowError = ref<string | null>(null);
  /** rowErrorIsProxy selects the formatter on locale switch. */
  const rowErrorIsProxy = ref(false);
  /**
   * addServerError keeps the add failure behind a banner so a locale switch
   * can re-derive it; rowServerError does the same for per-row actions.
   */
  const addServerError = ref<unknown>(null);
  const rowServerError = ref<unknown>(null);

  /**
   * tr resolves one applications message in the current locale. Reading
   * activeLocale pins the caller to the language switch.
   */
  function tr(key: string): string {
    void activeLocale.value;
    return String(i18n.global.t(key));
  }

  /** clearErrors drops every stale banner before a fresh attempt. */
  function clearErrors(): void {
    addError.value = null;
    addServerError.value = null;
    rowErrorId.value = null;
    rowError.value = null;
    rowErrorIsProxy.value = false;
    rowServerError.value = null;
  }
  /** refresh reloads the domain rows of the current application. */
  async function refresh(silent = false): Promise<void> {
    if (!silent) {
      loadingDomains.value = true;
    }
    try {
      domains.value = await listDomains(application.value.id);
      domainsError.value = null;
    } catch (error) {
      domainsError.value = describeApplicationError(error);
    } finally {
      loadingDomains.value = false;
    }
  }

  /** handleAddDomain validates locally, then attaches the hostname. */
  async function handleAddDomain(): Promise<void> {
    clearErrors();
    const next = newDomain.value.trim().toLowerCase();
    const issues = fieldErrors(hostDomainSchema, next);
    if (issues.length > 0) {
      addError.value = issues[0];
      return;
    }
    addingDomain.value = true;
    try {
      await addDomain(application.value.id, next);
      newDomain.value = "";
      await refresh(true);
      await appsStore.fetchApplication(application.value.id);
      bumpDomainsRefresh(refreshTick);
      message.success(tr("applications.detail.domainAdded"));
    } catch (error) {
      addServerError.value = error;
      addError.value = describeApplicationError(error);
    } finally {
      addingDomain.value = false;
    }
  }

  /** handleRemoveDomain detaches one hostname after confirmation. */
  async function handleRemoveDomain(domain: ApplicationDomain): Promise<void> {
    clearErrors();
    busyDomainId.value = domain.id;
    try {
      await removeDomain(application.value.id, domain.id);
      await refresh(true);
      await appsStore.fetchApplication(application.value.id);
      bumpDomainsRefresh(refreshTick);
      message.success(tr("applications.detail.domainRemoved"));
    } catch (error) {
      rowServerError.value = error;
      rowErrorId.value = domain.id;
      rowError.value = describeApplicationError(error);
    } finally {
      busyDomainId.value = null;
    }
  }

  /** handleSecureDomain creates an http-01 intent for one alias (N2). */
  async function handleSecureDomain(domain: ApplicationDomain): Promise<void> {
    clearErrors();
    busyDomainId.value = domain.id;
    try {
      await proxyStore.createCertificateConfig({
        application_id: application.value.id,
        domain: domain.domain,
      });
      bumpDomainsRefresh(refreshTick);
      message.success(tr("applications.detail.certCreated"));
    } catch (error) {
      rowServerError.value = error;
      rowErrorId.value = domain.id;
      rowErrorIsProxy.value = true;
      rowError.value = describeProxyError(error);
    } finally {
      busyDomainId.value = null;
    }
  }
  /** handleSetPrimary promotes one attached hostname to primary. */
  async function handleSetPrimary(domain: ApplicationDomain): Promise<void> {
    clearErrors();
    busyDomainId.value = domain.id;
    try {
      await setPrimaryDomain(application.value.id, domain.id);
      await refresh(true);
      await appsStore.fetchApplication(application.value.id);
      bumpDomainsRefresh(refreshTick);
      message.success(tr("applications.detail.primaryChanged"));
    } catch (error) {
      rowServerError.value = error;
      rowErrorId.value = domain.id;
      rowError.value = describeApplicationError(error);
    } finally {
      busyDomainId.value = null;
    }
  }

  const stopLocaleRefresh = onLocaleChange(() => {
    if (domainsError.value !== null) {
      void refresh(true);
    }
    if (addError.value !== null) {
      if (addServerError.value !== null) {
        addError.value = describeApplicationError(addServerError.value);
      } else {
        const issues = fieldErrors(
          hostDomainSchema,
          newDomain.value.trim().toLowerCase(),
        );
        addError.value = issues[0] ?? null;
      }
    }
    if (rowError.value !== null && rowServerError.value !== null) {
      rowError.value = rowErrorIsProxy.value
        ? describeProxyError(rowServerError.value)
        : describeApplicationError(rowServerError.value);
    }
  });
  onUnmounted(stopLocaleRefresh);

  watch(
    () => application.value.id,
    () => {
      newDomain.value = "";
      addError.value = null;
      rowError.value = null;
      void refresh();
    },
  );

  onMounted(() => {
    void refresh();
  });

  return {
    domains,
    loadingDomains,
    domainsError,
    newDomain,
    addingDomain,
    addError,
    busyDomainId,
    rowErrorId,
    rowError,
    refresh,
    handleAddDomain,
    handleRemoveDomain,
    handleSecureDomain,
    handleSetPrimary,
  };
}
