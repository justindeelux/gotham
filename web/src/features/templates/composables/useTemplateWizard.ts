import { useMessage } from "naive-ui";
import { computed, ref, toValue, watch } from "vue";
import type { Ref } from "vue";
import { useRouter } from "vue-router";

import { describeServiceError } from "@/features/services";
import {
  buildTemplateRenderValues,
  describeTemplateError,
  templateValuesFromFields,
  validateTemplateValues,
} from "@/features/templates/api/templates";
import type {
  TemplateDetail,
  TemplateField,
  TemplateRender,
  TemplateValues,
} from "@/features/templates/api/templates";
import { activeLocale, i18n } from "@/shared/i18n";
import { fieldErrors } from "@/shared/validation/naiveAdapter";
import { serviceNameSchema, serviceNodeSchema } from "@/shared/validation/primitives";
import { useServersStore } from "@/features/servers";
import { useServicesStore } from "@/features/services";
import { useTemplatesStore } from "@/features/templates/stores/templates";

export function useTemplateWizard(
  show: Ref<boolean>,
  slug: Ref<string>,
  scope: { projectId: string | Ref<string>; environmentId: string | Ref<string> } = {
    projectId: "",
    environmentId: "",
  },
  onClose: () => void = () => undefined,
): {
  step: Ref<number>;
  detail: Ref<TemplateDetail | null>;
  detailLoading: Ref<boolean>;
  detailError: Ref<string | null>;
  values: Ref<TemplateValues>;
  showErrors: Ref<boolean>;
  render: Ref<TemplateRender | null>;
  renderLoading: Ref<boolean>;
  renderError: Ref<string | null>;
  name: Ref<string>;
  scopeProjectId: Ref<string>;
  scopeEnvironmentId: Ref<string>;
  serverId: Ref<string>;
  createAttempted: Ref<boolean>;
  creating: Ref<boolean>;
  createError: Ref<string | null>;
  fields: Ref<TemplateField[]>;
  allErrors: Ref<Record<string, string>>;
  formErrors: Ref<Record<string, string>>;
  secretKeys: Ref<string[]>;
  nameError: Ref<string>;
  nodeError: Ref<string>;
  scopeError: Ref<string>;
  scopeValid: Ref<boolean>;
  next: () => void;
  handleCreate: () => Promise<void>;
  reset: () => void;
} {
  const templatesStore = useTemplatesStore();
  const servicesStore = useServicesStore();
  const serversStore = useServersStore();
  const message = useMessage();
  const router = useRouter();

  const step = ref(1);
  const detail = ref<TemplateDetail | null>(null);
  const detailLoading = ref(false);
  const detailFailure: Ref<unknown> = ref(null);
  const detailError = retained(detailFailure, describeTemplateError);
  const values = ref<TemplateValues>({});
  const showErrors = ref(false);

  const render = ref<TemplateRender | null>(null);
  const renderLoading = ref(false);
  const renderFailure: Ref<unknown> = ref(null);
  const renderError = retained(renderFailure, describeTemplateError);

  const name = ref("");
  const scopeProjectId = ref(toValue(scope.projectId));
  const scopeEnvironmentId = ref(toValue(scope.environmentId));
  const serverId = ref("");
  const createAttempted = ref(false);
  const creating = ref(false);
  const createFailure: Ref<unknown> = ref(null);
  const createError = retained(createFailure, describeServiceError);

  let renderToken = 0;

  const fields = computed<TemplateField[]>(() => detail.value?.fields ?? []);

  const allErrors = computed<Record<string, string>>(() =>
    validateTemplateValues(fields.value, values.value),
  );

  const formErrors = computed<Record<string, string>>(() =>
    showErrors.value ? allErrors.value : {},
  );

  const secretKeys = computed<string[]>(() =>
    Object.keys(render.value?.env ?? {}).sort(),
  );

  const nameError = computed<string>(() =>
    !createAttempted.value ? "" : (fieldErrors(serviceNameSchema, name.value)[0] ?? ""),
  );

  const nodeError = computed<string>(() =>
    !createAttempted.value ? "" : (fieldErrors(serviceNodeSchema, serverId.value)[0] ?? ""),
  );

  const scopeValid = computed<boolean>(() => scopeEnvironmentId.value !== "");

  const scopeError = computed<string>(() => {
    if (!createAttempted.value || scopeValid.value) {
      return "";
    }
    void activeLocale.value;
    return String(i18n.global.t("templates.target.scopeError"));
  });

  function retained(
    failure: Ref<unknown>,
    describe: (_error: unknown) => string,
  ): Ref<string | null> {
    return computed<string | null>(() => {
      if (failure.value === null) {
        return null;
      }
      void activeLocale.value;
      return describe(failure.value);
    });
  }

  async function open(): Promise<void> {
    reset();
    detailLoading.value = true;
    detailFailure.value = null;
    try {
      const template = await templatesStore.fetchDetail(slug.value);
      detail.value = template;
      values.value = templateValuesFromFields(template.fields);
      name.value = template.slug;
    } catch (error) {
      detailFailure.value = error;
    } finally {
      detailLoading.value = false;
    }
    void serversStore.fetchServers().catch(() => undefined);
  }

  function reset(): void {
    renderToken += 1;
    step.value = 1;
    detail.value = null;
    detailFailure.value = null;
    values.value = {};
    showErrors.value = false;
    render.value = null;
    renderFailure.value = null;
    renderLoading.value = false;
    name.value = "";
    seedScope();
    serverId.value = "";
    createAttempted.value = false;
    creating.value = false;
    createFailure.value = null;
  }

  function seedScope(): void {
    scopeProjectId.value = toValue(scope.projectId);
    scopeEnvironmentId.value = toValue(scope.environmentId);
  }

  async function loadRender(): Promise<void> {
    if (!detail.value) {
      return;
    }
    const token = ++renderToken;
    render.value = null;
    renderFailure.value = null;
    renderLoading.value = true;
    try {
      const rendered = await templatesStore.render(
        detail.value.slug,
        buildTemplateRenderValues(values.value),
      );
      if (token !== renderToken) {
        return;
      }
      render.value = rendered;
    } catch (error) {
      if (token !== renderToken) {
        return;
      }
      render.value = null;
      renderFailure.value = error;
    } finally {
      if (token === renderToken) {
        renderLoading.value = false;
      }
    }
  }

  function next(): void {
    if (step.value === 1) {
      showErrors.value = true;
      if (Object.keys(allErrors.value).length > 0) {
        return;
      }
      step.value = 2;
      void loadRender();
      return;
    }
    if (
      step.value === 2 &&
      !renderLoading.value &&
      render.value !== null
    ) {
      step.value = 3;
    }
  }

  async function handleCreate(): Promise<void> {
    createAttempted.value = true;
    if (creating.value) {
      return;
    }
    const rendered = render.value;
    if (
      renderLoading.value ||
      rendered === null ||
      !serviceNameSchema.safeParse(name.value).success ||
      !serviceNodeSchema.safeParse(serverId.value).success ||
      !scopeValid.value
    ) {
      return;
    }
    creating.value = true;
    createFailure.value = null;
    try {
      const service = await servicesStore.create({
        name: name.value.trim(),
        environment_id: scopeEnvironmentId.value,
        server_id: serverId.value,
        compose_yaml: rendered.compose_yaml,
        env: rendered.env,
        deploy: true,
        template: detail.value?.slug,
      });
      message.success(
        String(i18n.global.t("templates.toast.deploying", { name: service.name })),
      );
      onClose();
      await router?.push({
        name: "service-detail",
        params: {
          projectId: service.project_id,
          environmentId: service.environment_id,
          id: service.id,
        },
      });
    } catch (error) {
      createFailure.value = error;
    } finally {
      creating.value = false;
    }
  }

  watch(
    () => show.value,
    (visible) => {
      if (visible) {
        seedScope();
        void open();
      }
    },
  );

  watch(
    [() => toValue(scope.projectId), () => toValue(scope.environmentId)],
    () => seedScope(),
  );

  watch(
    () => slug.value,
    () => {
      if (show.value) {
        void open();
      }
    },
  );

  return {
    step,
    detail,
    detailLoading,
    detailError,
    values,
    showErrors,
    render,
    renderLoading,
    renderError,
    name,
    scopeProjectId,
    scopeEnvironmentId,
    serverId,
    createAttempted,
    creating,
    createError,
    fields,
    allErrors,
    formErrors,
    secretKeys,
    nameError,
    nodeError,
    scopeError,
    scopeValid,
    next,
    handleCreate,
    reset,
  };
}
