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
import { extractComposeServicePorts } from "@/shared/utils/compose";
import { describeProxyError, useProxyStore } from "@/features/domains";
import { isApiError } from "@/features/servers";
import { activeLocale, i18n, onLocaleChange } from "@/shared/i18n";
import { fieldErrors } from "@/shared/validation/naiveAdapter";

export interface DomainDraft {
  domain: string;
  port: number | null;
  service: string;
  protocol: DomainProtocol;
}

export function useApplicationDomains(application: Ref<Application>) {
  const message = useMessage();
  const appsStore = useApplicationsStore();
  const proxyStore = useProxyStore();
  const refreshTick = useDomainsRefresh();

  const domains = ref<ApplicationDomain[]>([]);
  const loadingDomains = ref(false);
  const domainsError = ref<string | null>(null);
  const formOpen = ref(false);
  const draft = reactive<DomainDraft>({
    domain: "",
    port: null,
    service: "",
    protocol: "http",
  });
  const editingId = ref<string | null>(null);
  const saving = ref(false);
  const formError = ref<string | null>(null);
  const busyDomainId = ref<string | null>(null);
  const rowErrorId = ref<string | null>(null);
  const rowError = ref<string | null>(null);
  const rowErrorIsProxy = ref(false);
  const formServerError = ref<unknown>(null);
  const rowServerError = ref<unknown>(null);
  let refreshSeq = 0;

  const isCompose = computed<boolean>(() => application.value.source_type === "compose");

  const composePorts = computed<Record<string, number>>(() =>
    extractComposeServicePorts(application.value.compose_content ?? ""),
  );

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
    rowErrorIsProxy.value = false;
    rowServerError.value = null;
  }

  function certificateIntent(host: string) {
    return (
      proxyStore.certificates.find(
        (item) => item.application_id === application.value.id && item.domain === host,
      ) ?? null
    );
  }

  async function refreshCertificateIntents(): Promise<void> {
    await proxyStore.fetchCertificates().catch(() => undefined);
  }

  async function removeCertificateIntent(host: string): Promise<unknown | null> {
    await refreshCertificateIntents();
    const intent = certificateIntent(host);
    if (intent === null) {
      return null;
    }
    try {
      await proxyStore.removeCertificate(intent.id);
      return null;
    } catch (error) {
      if (isApiError(error) && error.status === 404) {
        await refreshCertificateIntents();
        return null;
      }
      return error;
    }
  }

  function surfaceRowError(id: string | null, error: unknown, isProxy: boolean): void {
    rowServerError.value = error;
    rowErrorId.value = id;
    rowErrorIsProxy.value = isProxy;
    rowError.value = isProxy
      ? describeProxyError(error)
      : describeApplicationError(error);
  }

  async function refresh(silent = false): Promise<void> {
    const appID = application.value.id;
    const seq = ++refreshSeq;
    if (!silent) {
      loadingDomains.value = true;
    }
    try {
      const rows = await listDomains(appID);
      if (seq !== refreshSeq || application.value.id !== appID) {
        return;
      }
      domains.value = rows;
      domainsError.value = null;
      if (typeof proxyStore.fetchRouters === "function") {
        void proxyStore.fetchRouters().catch(() => undefined);
      }
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

  function routeDiagnostic(host: string): string {
    for (const node of proxyStore.routerNodes ?? []) {
      const match = node.diagnostics?.find(
        (item) =>
          item.application_id === application.value.id &&
          (item.domain ?? "") === host,
      );
      if (match?.reason) {
        return match.reason;
      }
    }
    return "";
  }

  function resetDraft(): void {
    editingId.value = null;
    draft.domain = "";
    draft.port = null;
    draft.service = "";
    draft.protocol = "http";
    formError.value = null;
    formServerError.value = null;
  }

  function openCreate(): void {
    clearErrors();
    resetDraft();
    formOpen.value = true;
  }

  function closeForm(): void {
    formOpen.value = false;
    resetDraft();
  }

  function startEdit(row: ApplicationDomain): void {
    clearErrors();
    editingId.value = row.id;
    draft.domain = row.domain;
    draft.port = row.port > 0 ? row.port : null;
    draft.service = row.service;
    draft.protocol = row.protocol;
    formOpen.value = true;
  }

  function handleServiceChange(service: string | null): void {
    draft.service = service ?? "";
    if (!isCompose.value) {
      return;
    }
    const port = composePorts.value[draft.service];
    draft.port = port && port > 0 ? port : null;
  }

  function validatedDraft(): DomainInput | string {
    const domain = draft.domain.trim().toLowerCase();
    const domainIssues = fieldErrors(hostDomainSchema, domain);
    if (domain === "" || domainIssues.length > 0) {
      return domainIssues[0] ?? tr("applications.validation.domain");
    }
    const portIssues = draft.port === null ? [] : fieldErrors(domainPortSchema, draft.port);
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
      port: draft.port ?? 0,
      service,
      protocol: draft.protocol,
    };
  }

  async function handleSave(): Promise<void> {
    clearErrors();
    const payload = validatedDraft();
    if (typeof payload === "string") {
      formError.value = payload;
      return;
    }
    const editingID = editingId.value;
    const editing =
      editingID === null
        ? null
        : domains.value.find((row) => row.id === editingID) ?? null;
    const oldHost = editing?.domain ?? "";
    const renamed = editing !== null && payload.domain !== oldHost;
    saving.value = true;
    try {
      if (editingID !== null) {
        await updateDomain(application.value.id, editingID, payload);
        message.success(tr("applications.detail.domainUpdated"));
      } else {
        await addDomain(application.value.id, payload);
        message.success(tr("applications.detail.domainAdded"));
      }
      const cleanupError = renamed ? await removeCertificateIntent(oldHost) : null;
      resetDraft();
      formOpen.value = false;
      await refresh(true);
      await appsStore.fetchApplication(application.value.id);
      bumpDomainsRefresh(refreshTick);
      if (cleanupError !== null) {
        surfaceRowError(null, cleanupError, true);
        if (rowError.value !== null) {
          message.error(rowError.value);
        }
      }
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
      const cleanupError = await removeCertificateIntent(domain.domain);
      if (editingId.value === domain.id) {
        resetDraft();
      }
      await refresh(true);
      await appsStore.fetchApplication(application.value.id);
      bumpDomainsRefresh(refreshTick);
      message.success(tr("applications.detail.domainRemoved"));
      if (cleanupError !== null) {
        surfaceRowError(domain.id, cleanupError, true);
        if (rowError.value !== null) {
          message.error(rowError.value);
        }
      }
    } catch (error) {
      surfaceRowError(domain.id, error, false);
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
      surfaceRowError(domain.id, error, false);
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
        const payload = validatedDraft();
        formError.value = typeof payload === "string" ? payload : null;
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
    routeDiagnostic,
  };
}
