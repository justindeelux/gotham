import { useMessage } from "naive-ui";
import { computed, onMounted, reactive, ref, watch, type ComputedRef } from "vue";

import {
  addServiceDomain,
  describeServiceError,
  listServiceDomains,
  removeServiceDomain,
  updateServiceDomain,
  type Service,
  type ServiceDomain,
  type ServiceDomainInput,
  type ServiceDomainProtocol,
} from "@/features/services/api/services";
import {
  defaultDomainTarget,
  serviceDomainHostSchema,
  serviceDomainPortSchema,
  serviceDomainProtocolSchema,
  serviceDomainTargetSchema,
  serviceDomainTargets,
} from "@/features/services/schemas/domains";
import { useServicesStore } from "@/features/services/stores/services";
import { isApiError } from "@/features/servers";
import { activeLocale, i18n } from "@/shared/i18n";
import { fieldErrors } from "@/shared/validation/naiveAdapter";

export interface ServiceDomainDraft {
  domain: string;
  target: string;
  port: number | null;
  protocol: ServiceDomainProtocol;
  enabled: boolean;
}

export interface DomainFieldErrors {
  domain: string;
  target: string;
  port: string;
}

const emptyErrors = (): DomainFieldErrors => ({ domain: "", target: "", port: "" });

export function useServiceDomains(
  service: ComputedRef<Service | null>,
  serviceId: ComputedRef<string>,
  canWrite: ComputedRef<boolean>,
) {
  const message = useMessage();
  const servicesStore = useServicesStore();

  const domains = ref<ServiceDomain[]>([]);
  const loading = ref(false);
  const loadFailure = ref<unknown>(null);
  const formOpen = ref(false);
  const editingId = ref<string | null>(null);
  const saving = ref(false);
  const formFailure = ref<unknown>(null);
  const formError = ref<string | null>(null);
  const errors = reactive<DomainFieldErrors>(emptyErrors());
  const busyDomainId = ref<string | null>(null);
  const draft = reactive<ServiceDomainDraft>({
    domain: "",
    target: "",
    port: null,
    protocol: "http",
    enabled: true,
  });
  let refreshSeq = 0;

  const error = computed<string | null>(() =>
    loadFailure.value === null ? null : describeServiceError(loadFailure.value),
  );

  const targets = computed(() =>
    serviceDomainTargets(service.value?.compose_yaml ?? ""),
  );

  function tr(key: string, params?: Record<string, string | number>): string {
    void activeLocale.value;
    return String(i18n.global.t(key, params ?? {}));
  }

  function clearErrors(): void {
    Object.assign(errors, emptyErrors());
    formFailure.value = null;
    formError.value = null;
  }

  async function refresh(silent = false): Promise<void> {
    const id = serviceId.value;
    const seq = ++refreshSeq;
    if (!silent) {
      loading.value = true;
    }
    try {
      const rows = await listServiceDomains(id);
      if (seq !== refreshSeq || serviceId.value !== id) {
        return;
      }
      domains.value = rows;
      loadFailure.value = null;
    } catch (err) {
      if (seq === refreshSeq) {
        loadFailure.value = err;
      }
    } finally {
      if (seq === refreshSeq) {
        loading.value = false;
      }
    }
  }

  async function reloadService(): Promise<void> {
    await servicesStore.fetchService(serviceId.value).catch(() => undefined);
  }

  function resetDraft(): void {
    editingId.value = null;
    draft.domain = "";
    draft.target = "";
    draft.port = null;
    draft.protocol = "http";
    draft.enabled = true;
    clearErrors();
  }

  function openCreate(): void {
    if (!canWrite.value) {
      return;
    }
    resetDraft();
    const preset = defaultDomainTarget(service.value?.compose_yaml ?? "");
    draft.target = preset?.service ?? targets.value[0]?.service ?? "";
    draft.port = preset?.port ?? null;
    formOpen.value = true;
  }

  function startEdit(row: ServiceDomain): void {
    if (!canWrite.value || row.source !== "managed") {
      return;
    }
    resetDraft();
    editingId.value = row.id ?? null;
    draft.domain = row.domain;
    draft.target = row.compose_service;
    draft.port = row.port > 0 ? row.port : null;
    draft.protocol = row.protocol;
    draft.enabled = !row.disabled;
    formOpen.value = true;
  }

  function closeForm(): void {
    formOpen.value = false;
    resetDraft();
  }

  function handleTargetChange(target: string | null): void {
    draft.target = target ?? "";
    const match = targets.value.find((item) => item.service === draft.target);
    draft.port = match && match.port > 0 ? match.port : null;
  }

  function validatedDraft(): ServiceDomainInput | null {
    Object.assign(errors, emptyErrors());
    const host = draft.domain.trim().toLowerCase();
    errors.domain = fieldErrors(serviceDomainHostSchema, host)[0] ?? "";
    errors.target = fieldErrors(serviceDomainTargetSchema, draft.target)[0] ?? "";
    errors.port =
      draft.port === null
        ? tr("services.validation.domainPort")
        : (fieldErrors(serviceDomainPortSchema, draft.port)[0] ?? "");
    if (errors.domain || errors.target || errors.port) {
      formError.value = errors.target || errors.port || null;
      return null;
    }
    const protocol = serviceDomainProtocolSchema.safeParse(draft.protocol);
    if (!protocol.success) {
      return null;
    }
    return {
      domain: host,
      compose_service: draft.target.trim(),
      port: draft.port ?? 0,
      protocol: protocol.data,
      disabled: !draft.enabled,
    };
  }

  function surfaceFormError(err: unknown): void {
    formFailure.value = err;
    const text = describeServiceError(err);
    const hostIssue =
      isApiError(err) &&
      (err.status === 409 || err.message.toLowerCase().includes("domain"));
    if (hostIssue) {
      errors.domain = text;
      return;
    }
    formError.value = text;
  }

  async function handleSave(): Promise<void> {
    clearErrors();
    const payload = validatedDraft();
    if (payload === null) {
      return;
    }
    saving.value = true;
    try {
      if (editingId.value !== null) {
        await updateServiceDomain(serviceId.value, editingId.value, payload);
        message.success(tr("services.toast.domainSaved"));
      } else {
        await addServiceDomain(serviceId.value, payload);
        message.success(tr("services.toast.domainAdded"));
      }
      closeForm();
      await refresh(true);
      await reloadService();
    } catch (err) {
      surfaceFormError(err);
    } finally {
      saving.value = false;
    }
  }

  async function handleToggle(row: ServiceDomain, enabled: boolean): Promise<void> {
    if (!canWrite.value || row.id === undefined) {
      return;
    }
    busyDomainId.value = row.id;
    loadFailure.value = null;
    try {
      await updateServiceDomain(serviceId.value, row.id, {
        domain: row.domain,
        compose_service: row.compose_service,
        port: row.port,
        protocol: row.protocol,
        disabled: !enabled,
      });
      await refresh(true);
      await reloadService();
    } catch (err) {
      loadFailure.value = err;
    } finally {
      busyDomainId.value = null;
    }
  }

  async function handleRemove(row: ServiceDomain): Promise<void> {
    if (!canWrite.value || row.id === undefined) {
      return;
    }
    busyDomainId.value = row.id;
    loadFailure.value = null;
    try {
      await removeServiceDomain(serviceId.value, row.id);
      await refresh(true);
      await reloadService();
      message.success(tr("services.toast.domainRemoved"));
    } catch (err) {
      loadFailure.value = err;
    } finally {
      busyDomainId.value = null;
    }
  }

  function statusLabel(row: ServiceDomain): string {
    return tr(`services.domains.status.${row.status}`);
  }

  watch(serviceId, () => {
    closeForm();
    domains.value = [];
    void refresh();
  });

  onMounted(() => {
    void refresh();
  });

  return {
    domains,
    loading,
    error,
    formOpen,
    editingId,
    draft,
    saving,
    formError,
    errors,
    busyDomainId,
    targets,
    openCreate,
    startEdit,
    closeForm,
    handleTargetChange,
    handleSave,
    handleToggle,
    handleRemove,
    statusLabel,
  };
}
