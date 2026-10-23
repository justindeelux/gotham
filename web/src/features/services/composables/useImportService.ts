import { useMessage } from "naive-ui";
import { computed, ref, toValue, watch } from "vue";
import type { Ref } from "vue";

import { describeServiceError } from "@/features/services/api/services";
import { activeLocale, i18n } from "@/shared/i18n";
import type { Service } from "@/features/services/api/services";
import { serviceNameSchema, serviceNodeSchema } from "@/shared/validation/primitives";
import { fieldErrors } from "@/shared/validation/naiveAdapter";
import { useServicesStore } from "@/features/services/stores/services";

export function useImportService(
  show: Ref<boolean>,
  scope: {
    projectId: string | Ref<string>;
    environmentId: string | Ref<string>;
  } = { projectId: "", environmentId: "" },
  onCreated: (_service: Service) => void = () => undefined,
) {
  const message = useMessage();
  const servicesStore = useServicesStore();

  const name = ref("");
  const scopeProjectId = ref(toValue(scope.projectId));
  const scopeEnvironmentId = ref(toValue(scope.environmentId));
  const serverId = ref("");
  const yaml = ref("");
  const attempted = ref(false);
  const importing = ref(false);
  const importFailure: Ref<unknown> = ref(null);
  const importError = computed<string | null>(() => {
    if (importFailure.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeServiceError(importFailure.value);
  });

  const envReference = "${VAR}";

  const nameError = computed<string>(() =>
    !attempted.value ? "" : (fieldErrors(serviceNameSchema, name.value)[0] ?? ""),
  );

  const nodeError = computed<string>(() =>
    !attempted.value ? "" : (fieldErrors(serviceNodeSchema, serverId.value)[0] ?? ""),
  );

  const scopeError = computed<string>(() => {
    if (!attempted.value || scopeEnvironmentId.value !== "") {
      return "";
    }
    void activeLocale.value;
    return String(i18n.global.t("services.import.scopeError"));
  });

  function reset(): void {
    name.value = "";
    seedScope();
    serverId.value = "";
    yaml.value = "";
    attempted.value = false;
    importFailure.value = null;
    importing.value = false;
  }

  function seedScope(): void {
    scopeProjectId.value = toValue(scope.projectId);
    scopeEnvironmentId.value = toValue(scope.environmentId);
  }

  async function handleImport(): Promise<void> {
    attempted.value = true;
    if (importing.value) {
      return;
    }
    if (
      !serviceNameSchema.safeParse(name.value).success ||
      !serviceNodeSchema.safeParse(serverId.value).success ||
      scopeEnvironmentId.value === ""
    ) {
      return;
    }
    importing.value = true;
    importFailure.value = null;
    try {
      const created = await servicesStore.create({
        name: name.value.trim(),
        environment_id: scopeEnvironmentId.value,
        server_id: serverId.value,
        compose_yaml: yaml.value,
      });
      message.success(String(i18n.global.t("services.toast.created", { name: created.name })));
      onCreated(created);
    } catch (error) {
      importFailure.value = error;
    } finally {
      importing.value = false;
    }
  }

  watch(show, (visible) => {
    if (visible) {
      reset();
    }
  });

  watch(
    [() => toValue(scope.projectId), () => toValue(scope.environmentId)],
    () => seedScope(),
  );

  return {
    name,
    scopeProjectId,
    scopeEnvironmentId,
    serverId,
    yaml,
    attempted,
    importing,
    importError,
    envReference,
    nameError,
    nodeError,
    scopeError,
    handleImport,
    reset,
  };
}

export type ImportServiceState = ReturnType<typeof useImportService>;
