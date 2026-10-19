import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, reactive, ref, watch, type Ref } from "vue";

import {
  addDomain,
  describeApplicationError,
  listDomains,
  removeDomain,
  setPrimaryDomain,
  updateDomain,
  type Application,
  type ApplicationDomain,
  type DomainInput,
  type DomainProtocol,
} from "@/features/applications/api/applications";
import {
  domainPortSchema,
  domainProtocolSchema,
  domainServiceSchema,
  hostDomainSchema,
} from "@/features/applications/schemas/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import {
  bumpDomainsRefresh,
  useDomainsRefresh,
} from "@/features/applications/composables/domainRefresh";
import { extractComposeServicePorts } from "@/features/applications/utils/compose";
import { activeLocale, i18n, onLocaleChange } from "@/shared/i18n";
import { fieldErrors } from "@/shared/validation/naiveAdapter";

/** Draft shape of the add/edit domain form. */
export interface DomainDraft {
  domain: string;
  port: number | null;
  service: string;
  protocol: DomainProtocol;
}

/**
 * Domain routing behind the Domains tab (JUS-236, formerly JUS-233): every
 * hostname carries its own target port, optional compose service and
 * protocol. The same modal adds and edits; selecting a compose service
 * pre-fills its declared port.
 */
export function useApplicationDomains(application: Ref<Application>) {
  const message = useMessage();
  const appsStore = useApplicationsStore();
  /** Captured during setup: inject() has no active instance after an await. */
  const refreshTick = useDomainsRefresh();

  const domains = ref<ApplicationDomain[]>([]);
  const loadingDomains = ref(false);
  const domainsError = ref<string | null>(null);
  /** formOpen drives the shared add/edit modal. */
  const formOpen = ref(false);
  const draft = reactive<DomainDraft>({
    domain: "",
    port: 80,
    service: "",
    protocol: "http",
  });
  const editingId = ref<string | null>(null);
  const saving = ref(false);
  const formError = ref<string | null>(null);
  const busyDomainId = ref<string | null>(null);
  const rowErrorId = ref<string | null>(null);
  const rowError = ref<string | null>(null);
  const formServerError = ref<unknown>(null);
  const rowServerError = ref<unknown>(null);
  /** refreshSeq drops an out-of-order response after an application switch. */
  let refreshSeq = 0;

  /** isCompose reports whether the application has compose-specific fields. */
  const isCompose = computed<boolean>(() => application.value.source_type === "compose");

  /**
   * composePorts maps each compose service to its declared container port.
   * Selecting a service pre-fills the port (still editable); repo-backed
   * documents cannot be parsed here, so the selector falls back to the
   * release service and free text.
   */
  const composePorts = computed<Record<string, number>>(() =>
    extractComposeServicePorts(application.value.compose_content ?? ""),
  );

  /** serviceOptions lists the parsed services plus the release web service. */
  const serviceOptions = computed<Array<{ label: string; value: string }>>(() => {
    const names = new Set(Object.keys(composePorts.value));
    if (application.value.compose_service) {
      names.add(application.value.compose_service);
    }
    return [...names].map((name) => ({ label: name, value: name }));
  });

  function tr(key: string): string {
    void activeLocale.value;
    return String(i18n.global.t(key));
  }

  function clearErrors(): void {
    formError.value = null;
    formServerError.value = null;
    rowErrorId.value = null;
    rowError.value = null;
    rowServerError.value = null;
  }

  async function refresh(silent = false): Promise<void> {
    const appID = application.value.id;
    const seq = ++refreshSeq;
    if (!silent) {
      loadingDomains.value = true;
    }
    try {
      const rows = await listDomains(appID);
      // A slow response for the previous application must never overwrite the
      // new application's list.
      if (seq !== refreshSeq || application.value.id !== appID) {
        return;
      }
      domains.value = rows;
      domainsError.value = null;
    } catch (error) {
      if (seq !== refreshSeq || application.value.id !== appID) {
        return;
      }
      domainsError.value = describeApplicationError(error);
    } finally {
      if (seq === refreshSeq) {
        loadingDomains.value = false;
      }
    }
  }

  /** resetDraft clears the form back to the add-mode defaults. */
  function resetDraft(): void {
    editingId.value = null;
    draft.domain = "";
    draft.port = 80;
    draft.service = "";
    draft.protocol = "http";
    formError.value = null;
    formServerError.value = null;
  }

  /** openCreate opens the shared modal in add mode with a clean draft. */
  function openCreate(): void {
    clearErrors();
    resetDraft();
    formOpen.value = true;
  }

  /** closeForm dismisses the shared modal and drops the draft. */
  function closeForm(): void {
    formOpen.value = false;
    resetDraft();
  }

  /** startEdit loads one row into the shared add/edit modal. */
  function startEdit(row: ApplicationDomain): void {
    clearErrors();
    editingId.value = row.id;
    draft.domain = row.domain;
    draft.port = row.port;
    draft.service = row.service;
    draft.protocol = row.protocol;
    formOpen.value = true;
  }

  /** handleServiceChange pre-fills the port of the selected service. A
   * cleared select emits null; treat it as the release-default empty service.
   * A service without a declared port clears the field so a stale port from
   * the previous selection never looks pre-filled. */
  function handleServiceChange(service: string | null): void {
    draft.service = service ?? "";
    if (!isCompose.value) {
      return;
    }
    const port = composePorts.value[draft.service];
    draft.port = port && port > 0 ? port : null;
  }

  /** validatedDraft returns the normalized payload, or the first error. */
  function validatedDraft(): DomainInput | string {
    const domain = draft.domain.trim().toLowerCase();
    const domainIssues = fieldErrors(hostDomainSchema, domain);
    if (domain === "" || domainIssues.length > 0) {
      return domainIssues[0] ?? tr("applications.validation.domain");
    }
    const portIssues = fieldErrors(domainPortSchema, draft.port);
    if (portIssues.length > 0) {
      return portIssues[0];
    }
    const service = isCompose.value ? (draft.service ?? "").trim() : "";
    const serviceIssues = fieldErrors(domainServiceSchema, service);
    if (serviceIssues.length > 0) {
      return serviceIssues[0];
    }
    const protocolIssues = fieldErrors(domainProtocolSchema, draft.protocol);
    if (protocolIssues.length > 0) {
      return protocolIssues[0];
    }
    return {
      domain,
      port: draft.port as number,
      service,
      protocol: draft.protocol,
    };
  }

  /** handleSave creates or edits one domain. */
  async function handleSave(): Promise<void> {
    clearErrors();
    const payload = validatedDraft();
    if (typeof payload === "string") {
      formError.value = payload;
      return;
    }
    saving.value = true;
    try {
      if (editingId.value !== null) {
        await updateDomain(application.value.id, editingId.value, payload);
        message.success(tr("applications.detail.domainUpdated"));
      } else {
        await addDomain(application.value.id, payload);
        message.success(tr("applications.detail.domainAdded"));
      }
      resetDraft();
      formOpen.value = false;
      await refresh(true);
      await appsStore.fetchApplication(application.value.id);
      bumpDomainsRefresh(refreshTick);
    } catch (error) {
      formServerError.value = error;
      formError.value = describeApplicationError(error);
    } finally {
      saving.value = false;
    }
  }

  async function handleRemoveDomain(domain: ApplicationDomain): Promise<void> {
    clearErrors();
    busyDomainId.value = domain.id;
    try {
      await removeDomain(application.value.id, domain.id);
      if (editingId.value === domain.id) {
        resetDraft();
      }
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
    if (formError.value !== null) {
      if (formServerError.value !== null) {
        formError.value = describeApplicationError(formServerError.value);
      } else {
        // A local validation message is a catalog key: re-derive it from the
        // unchanged draft in the new locale.
        const payload = validatedDraft();
        formError.value = typeof payload === "string" ? payload : null;
      }
    }
    if (rowError.value !== null && rowServerError.value !== null) {
      rowError.value = describeApplicationError(rowServerError.value);
    }
  });
  onUnmounted(stopLocaleRefresh);

  watch(
    () => application.value.id,
    () => {
      resetDraft();
      formOpen.value = false;
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
    formOpen,
    draft,
    editingId,
    saving,
    formError,
    busyDomainId,
    rowErrorId,
    rowError,
    isCompose,
    serviceOptions,
    refresh,
    resetDraft,
    openCreate,
    closeForm,
    startEdit,
    handleServiceChange,
    handleSave,
    handleRemoveDomain,
    handleSetPrimary,
  };
}
