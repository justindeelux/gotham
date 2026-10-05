import { useMessage } from "naive-ui";
import { computed, ref, toValue, watch } from "vue";
import type { Ref } from "vue";

import { describeServiceError } from "@/features/services/api/services";
import type { Service } from "@/features/services/api/services";
import { serviceNameSchema, serviceNodeSchema } from "@/shared/validation/primitives";
import { fieldErrors } from "@/shared/validation/naiveAdapter";
import { useServicesStore } from "@/features/services/stores/services";

/**
 * Compose-import dialog state behind ImportComposeDialog (PE-5, Linear
 * JUS-34). The dialog is hosted by the environment page with the route's
 * scope; the scope stays changeable through the shared summary. On success
 * the created service goes to `onCreated` — the host navigates to the
 * nested detail page.
 */
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
  const importError = ref<string | null>(null);

  /** envReference is the compose `${VAR}` substitution form shown in copy. */
  const envReference = "${VAR}";

  const nameError = computed<string>(() =>
    !attempted.value ? "" : (fieldErrors(serviceNameSchema, name.value)[0] ?? ""),
  );

  const nodeError = computed<string>(() =>
    !attempted.value ? "" : (fieldErrors(serviceNodeSchema, serverId.value)[0] ?? ""),
  );

  const scopeError = computed<string>(() =>
    !attempted.value || scopeEnvironmentId.value !== ""
      ? ""
      : "Select a project and environment.",
  );

  /** reset restores the dialog to the live host scope with an empty form. */
  function reset(): void {
    name.value = "";
    seedScope();
    serverId.value = "";
    yaml.value = "";
    attempted.value = false;
    importError.value = null;
    importing.value = false;
  }

  /** seedScope copies the live route scope into the form. */
  function seedScope(): void {
    scopeProjectId.value = toValue(scope.projectId);
    scopeEnvironmentId.value = toValue(scope.environmentId);
  }

  /** handleImport stores the pasted document as a new service. */
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
    importError.value = null;
    try {
      const created = await servicesStore.create({
        name: name.value.trim(),
        environment_id: scopeEnvironmentId.value,
        server_id: serverId.value,
        compose_yaml: yaml.value,
      });
      message.success(`Service ${created.name} created.`);
      onCreated(created);
    } catch (error) {
      importError.value = describeServiceError(error);
    } finally {
      importing.value = false;
    }
  }

  watch(show, (visible) => {
    if (visible) {
      reset();
    }
  });

  // A route change while the dialog is mounted re-seeds the scope, so the
  // summary and the payload always name the current environment.
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

/** ImportServiceState is the dialog state owned by the host page. */
export type ImportServiceState = ReturnType<typeof useImportService>;
