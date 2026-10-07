import { useMessage } from "naive-ui";
import { computed, ref, toValue, watch } from "vue";
import type { Ref } from "vue";

import { describeServiceError } from "@/features/services";
import type { Service } from "@/features/services";
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

/**
 * Template deploy wizard state: dynamic config form (step 1) → rendered
 * compose preview (step 2) → name + node (step 3).
 *
 * The render contract is the critical part (see templates.ts): secret field
 * values arrive in `render.env` and are sent, unchanged and only there, as the
 * service's environment on create. They are never rendered, logged or copied
 * anywhere else, and the preview shows only the `${key}` references that stay
 * in `compose_yaml`.
 *
 * Deploy is a separate, explicit action after the create: it answers with the
 * finished attempt (the API deploys synchronously), so the live log pane
 * streams the project's container log via the services logs endpoint
 * afterwards. A failed deploy is shown as it comes back — never faked.
 */
export function useTemplateWizard(
  show: Ref<boolean>,
  slug: Ref<string>,
  scope: { projectId: string | Ref<string>; environmentId: string | Ref<string> } = {
    projectId: "",
    environmentId: "",
  },
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
  created: Ref<Service | null>;
  deploying: Ref<boolean>;
  deployError: Ref<string | null>;
  deployed: Ref<boolean>;
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
  handleDeploy: () => Promise<void>;
  reset: () => void;
} {
  const templatesStore = useTemplatesStore();
  const servicesStore = useServicesStore();
  const serversStore = useServersStore();
  const message = useMessage();

  const step = ref(1);
  const detail = ref<TemplateDetail | null>(null);
  const detailLoading = ref(false);
  /**
   * Raw refusals retained per surface; each display error derives from its
   * refusal in the active locale (see retained below), so an open wizard
   * refreshes on a language switch without losing values or refetching.
   */
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
  const created = ref<Service | null>(null);

  const deploying = ref(false);
  const deployFailure: Ref<unknown> = ref(null);
  const deployError = retained(deployFailure, describeServiceError);
  const deployed = ref(false);

  /**
   * renderToken invalidates obsolete renders: every request captures the token
   * it started with, and a completion whose token no longer matches (a newer
   * render, or a reset/close) is dropped instead of overwriting the state.
   */
  let renderToken = 0;

  const fields = computed<TemplateField[]>(() => detail.value?.fields ?? []);

  /** allErrors is the full field validation result; see showErrors below. */
  const allErrors = computed<Record<string, string>>(() =>
    validateTemplateValues(fields.value, values.value),
  );

  /** formErrors reveals messages only after the first Next attempt. */
  const formErrors = computed<Record<string, string>>(() =>
    showErrors.value ? allErrors.value : {},
  );

  /** secretKeys names the environment keys render held back from the document. */
  const secretKeys = computed<string[]>(() =>
    Object.keys(render.value?.env ?? {}).sort(),
  );

  const nameError = computed<string>(() =>
    !createAttempted.value ? "" : (fieldErrors(serviceNameSchema, name.value)[0] ?? ""),
  );

  const nodeError = computed<string>(() =>
    !createAttempted.value ? "" : (fieldErrors(serviceNodeSchema, serverId.value)[0] ?? ""),
  );

  /** scopeValid gates the create: the API requires an environment. */
  const scopeValid = computed<boolean>(() => scopeEnvironmentId.value !== "");

  const scopeError = computed<string>(() => {
    if (!createAttempted.value || scopeValid.value) {
      return "";
    }
    // Tracks the locale when called during render or inside a computed.
    void activeLocale.value;
    return String(i18n.global.t("templates.target.scopeError"));
  });

  /**
   * retained derives display text from a raw refusal in the active locale
   * through the caller's describe helper, so an open wizard refreshes on a
   * language switch without losing values or refetching.
   */
  function retained(
    failure: Ref<unknown>,
    describe: (_error: unknown) => string,
  ): Ref<string | null> {
    return computed<string | null>(() => {
      if (failure.value === null) {
        return null;
      }
      // Tracks the locale when called during render or inside a computed.
      void activeLocale.value;
      return describe(failure.value);
    });
  }

  /** open loads the template schema and seeds the form with its defaults. */
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

  /** reset drops every wizard value, including the secret-bearing ones. */
  function reset(): void {
    // Invalidate any in-flight render first: its response must not land in a
    // wizard that has been closed or reopened for another template.
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
    created.value = null;
    deploying.value = false;
    deployFailure.value = null;
    deployed.value = false;
  }

  /** seedScope copies the live route scope into the form. */
  function seedScope(): void {
    scopeProjectId.value = toValue(scope.projectId);
    scopeEnvironmentId.value = toValue(scope.environmentId);
  }

  /**
   * loadRender renders the form values and shows the preview document. The
   * previous preview is invalidated immediately, so nothing can be created from
   * an obsolete document while this request is in flight; navigation and create
   * are blocked on `renderLoading` instead. A completion that lost its token is
   * ignored.
   */
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

  /** next validates step 1 and moves forward. */
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

  /** handleCreate stores the service with the rendered document and its env. */
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
      created.value = await servicesStore.create({
        name: name.value.trim(),
        environment_id: scopeEnvironmentId.value,
        server_id: serverId.value,
        compose_yaml: rendered.compose_yaml,
        env: rendered.env,
      });
      message.success(String(i18n.global.t("templates.toast.created", { name: created.value.name })));
    } catch (error) {
      createFailure.value = error;
    } finally {
      creating.value = false;
    }
  }

  /** handleDeploy sends the project to the node and reports what came back. */
  async function handleDeploy(): Promise<void> {
    const service = created.value;
    if (service === null) {
      return;
    }
    deploying.value = true;
    deployFailure.value = null;
    try {
      await servicesStore.deploy(service.id);
      deployed.value = true;
      message.success(String(i18n.global.t("templates.toast.deployed")));
    } catch (error) {
      deployFailure.value = error;
    } finally {
      deploying.value = false;
    }
  }

  watch(
    () => show.value,
    (visible) => {
      if (visible) {
        // Re-seed from the live scope: the route may have moved while the
        // wizard was closed, and reset-on-close alone would keep the stale
        // environment for the next open.
        seedScope();
        void open();
      }
    },
  );

  // A route change while the wizard is mounted re-seeds the scope, so the
  // summary and the payload always name the current environment.
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
    created,
    deploying,
    deployError,
    deployed,
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
    handleDeploy,
    reset,
  };
}
