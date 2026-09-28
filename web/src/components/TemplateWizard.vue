<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NSpin,
  NStep,
  NSteps,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import { computed, ref, watch } from "vue";
import { RouterLink } from "vue-router";

import { describeServiceError } from "../api/services";
import type { Service } from "../api/services";
import {
  buildTemplateRenderValues,
  describeTemplateError,
  templateValuesFromFields,
  validateTemplateValues,
} from "../api/templates";
import type {
  TemplateDetail,
  TemplateField,
  TemplateRender,
  TemplateValues,
} from "../api/templates";
import DynamicForm from "./DynamicForm.vue";
import ServiceLogs from "./ServiceLogs.vue";
import { useServersStore } from "../stores/servers";
import { useServicesStore } from "../stores/services";
import { useTemplatesStore } from "../stores/templates";

/**
 * Template deploy wizard: dynamic config form (step 1) → rendered compose
 * preview (step 2) → name + node (step 3).
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

interface Props {
  show: boolean;
  slug: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const templatesStore = useTemplatesStore();
const servicesStore = useServicesStore();
const serversStore = useServersStore();
const message = useMessage();

const step = ref(1);
const detail = ref<TemplateDetail | null>(null);
const detailLoading = ref(false);
const detailError = ref<string | null>(null);
const values = ref<TemplateValues>({});
const showErrors = ref(false);

const render = ref<TemplateRender | null>(null);
const renderLoading = ref(false);
const renderError = ref<string | null>(null);

const name = ref("");
const serverId = ref("");
const createAttempted = ref(false);
const creating = ref(false);
const createError = ref<string | null>(null);
const created = ref<Service | null>(null);

const deploying = ref(false);
const deployError = ref<string | null>(null);
const deployed = ref(false);

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

const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
  serversStore.servers.map((server) => ({
    label: `${server.name} · ${server.ip}`,
    value: server.id,
  })),
);

const nameError = computed<string>(() =>
  createAttempted.value && name.value.trim() === "" ? "Enter a service name." : "",
);

const nodeError = computed<string>(() =>
  createAttempted.value && serverId.value === "" ? "Select a node." : "",
);

/** open loads the template schema and seeds the form with its defaults. */
async function open(): Promise<void> {
  reset();
  detailLoading.value = true;
  detailError.value = null;
  try {
    const template = await templatesStore.fetchDetail(props.slug);
    detail.value = template;
    values.value = templateValuesFromFields(template.fields);
    name.value = template.slug;
  } catch (error) {
    detailError.value = describeTemplateError(error);
  } finally {
    detailLoading.value = false;
  }
  void serversStore.fetchServers().catch(() => undefined);
}

/** reset drops every wizard value, including the secret-bearing ones. */
function reset(): void {
  step.value = 1;
  detail.value = null;
  detailError.value = null;
  values.value = {};
  showErrors.value = false;
  render.value = null;
  renderError.value = null;
  renderLoading.value = false;
  name.value = "";
  serverId.value = "";
  createAttempted.value = false;
  creating.value = false;
  createError.value = null;
  created.value = null;
  deploying.value = false;
  deployError.value = null;
  deployed.value = false;
}

/** loadRender renders the form values and shows the preview document. */
async function loadRender(): Promise<void> {
  if (!detail.value) {
    return;
  }
  renderLoading.value = true;
  renderError.value = null;
  try {
    render.value = await templatesStore.render(
      detail.value.slug,
      buildTemplateRenderValues(values.value),
    );
  } catch (error) {
    render.value = null;
    renderError.value = describeTemplateError(error);
  } finally {
    renderLoading.value = false;
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
  if (step.value === 2 && render.value !== null) {
    step.value = 3;
  }
}

/** handleCreate stores the service with the rendered document and its env. */
async function handleCreate(): Promise<void> {
  createAttempted.value = true;
  const rendered = render.value;
  if (rendered === null || name.value.trim() === "" || serverId.value === "") {
    return;
  }
  creating.value = true;
  createError.value = null;
  try {
    created.value = await servicesStore.create({
      name: name.value.trim(),
      server_id: serverId.value,
      compose_yaml: rendered.compose_yaml,
      env: rendered.env,
    });
    message.success(`Service ${created.value.name} created.`);
  } catch (error) {
    createError.value = describeServiceError(error);
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
  deployError.value = null;
  try {
    await servicesStore.deploy(service.id);
    deployed.value = true;
    message.success("Deploy finished.");
  } catch (error) {
    deployError.value = describeServiceError(error);
  } finally {
    deploying.value = false;
  }
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      void open();
    }
  },
);

watch(
  () => props.slug,
  () => {
    if (props.show) {
      void open();
    }
  },
);
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    class="template-wizard"
    style="width: 780px; max-width: 96vw"
    :closable="!creating && !deploying"
    :mask-closable="!creating && !deploying"
    @update:show="(value: boolean) => emit('update:show', value)"
    @after-leave="reset"
  >
    <template #header>
      Deploy template {{ detail?.name ?? slug }}
    </template>

    <NSpin :show="detailLoading">
      <NAlert v-if="detailError" type="error" :show-icon="true">
        {{ detailError }}
      </NAlert>

      <div v-else-if="detail" class="wizard">
        <NSteps :current="step" size="small">
          <NStep title="Configure" description="Fill the template fields" />
          <NStep title="Compose preview" description="Rendered from the schema" />
          <NStep title="Create" description="Name the service and pick a node" />
        </NSteps>

        <section v-if="step === 1" class="wizard__step" data-testid="wizard-step-1">
          <p class="wizard__desc">
            Fields come from the template schema
            (<span class="mono">GET /api/v1/templates/{{ detail.slug }}</span>);
            adding a template does not require a UI change. Secret values stay
            in this form until they are sent as the service environment.
          </p>
          <DynamicForm
            v-model="values"
            :fields="fields"
            :errors="formErrors"
          />
        </section>

        <section v-else-if="step === 2" class="wizard__step" data-testid="wizard-step-2">
          <NSpin :show="renderLoading">
            <NAlert v-if="renderError" type="error" :show-icon="true">
              {{ renderError }}
            </NAlert>
            <template v-else-if="render">
              <NSpace :size="8" align="center">
                <NTag size="small">{{ render.spec.services.length }} services</NTag>
                <NTag size="small">
                  {{ render.spec.named_volumes.length }} named volumes
                </NTag>
                <NTag size="small">
                  {{ render.spec.domains.length }} domain routes
                </NTag>
              </NSpace>
              <pre class="wizard__preview mono" data-testid="compose-preview">{{
                render.compose_yaml
              }}</pre>
              <NAlert type="info" :show-icon="true">
                The engine only substitutes strings. A secret field stays a
                <span class="mono">${field}</span> reference in this document;
                its value travels separately in the service environment and is
                redacted from errors and deploy history.
                <template v-if="secretKeys.length > 0">
                  Held separately: <span class="mono">{{ secretKeys.join(", ") }}</span
                  >.
                </template>
              </NAlert>
            </template>
          </NSpin>
        </section>

        <section v-else class="wizard__step" data-testid="wizard-step-3">
          <template v-if="created === null">
            <NAlert
              v-if="!serversStore.loading && serversStore.servers.length === 0"
              type="warning"
              :show-icon="true"
            >
              No node is registered yet.
              <RouterLink to="/servers">Add a server</RouterLink>
              before creating a service.
            </NAlert>
            <NForm label-placement="top" class="wizard__metaform">
              <NFormItem
                label="Service name"
                required
                :feedback="nameError"
                :validation-status="nameError ? 'error' : undefined"
                class="field-service-name"
              >
                <NInput v-model:value="name" aria-label="Service name" />
              </NFormItem>
              <NFormItem
                label="Node"
                required
                :feedback="nodeError"
                :validation-status="nodeError ? 'error' : undefined"
                class="field-service-node"
              >
                <NSelect
                  v-model:value="serverId"
                  :options="serverOptions"
                  :loading="serversStore.loading"
                  placeholder="Select a node"
                  aria-label="Node"
                />
              </NFormItem>
            </NForm>
            <NAlert v-if="createError" type="error" :show-icon="true">
              {{ createError }}
            </NAlert>
            <p class="wizard__desc">
              Creating stores the rendered document and its environment
              (<span class="mono">POST /api/v1/services</span>); a service runs
              on exactly one node. Deploy sends the project to that node's agent
              and shows what the agent reports back.
            </p>
          </template>

          <template v-else>
            <div class="wizard__success" data-testid="wizard-created">
              <h4>{{ created.name }} created</h4>
              <NSpace :size="8" align="center">
                <NTag size="small" type="warning">{{ created.status }}</NTag>
                <NTag size="small" class="mono">{{ created.project_name }}</NTag>
                <NTag
                  v-for="route in created.domains"
                  :key="route.domain"
                  size="small"
                  class="mono"
                >
                  {{ route.domain }}
                </NTag>
              </NSpace>
              <p class="wizard__desc">
                The service row exists; nothing runs yet. Deploy renders the
                document again on the node and records one deploy row.
              </p>
              <NAlert v-if="deployError" type="error" :show-icon="true">
                {{ deployError }}
              </NAlert>
              <NSpace :size="8" align="center">
                <RouterLink
                  :to="{ name: 'service-detail', params: { id: created.id } }"
                  @click="emit('update:show', false)"
                >
                  <NButton size="small">Open service detail</NButton>
                </RouterLink>
                <NTag v-if="deployed" size="small" type="success">deployed</NTag>
              </NSpace>
              <div class="wizard__stub">
                <h5>Deploy step timeline — backend pending</h5>
                <p>
                  The API records one row per deploy (state, error, timestamps)
                  and exposes no per-step progress, so no step timeline is shown
                  here. The detail page lists the history instead.
                </p>
              </div>
              <ServiceLogs
                v-if="deployed"
                :service-id="created.id"
                :services="render?.spec.services ?? []"
                :title="`${created.name} logs`"
              />
            </div>
          </template>
        </section>

        <div class="wizard__foot">
          <NButton
            v-if="step > 1 && created === null"
            size="small"
            @click="step -= 1"
          >
            Back
          </NButton>
          <NText depth="3" class="wizard__counter">
            Step {{ step }} / 3
          </NText>
          <span class="wizard__spacer"></span>
          <NButton size="small" @click="emit('update:show', false)">Close</NButton>
          <NButton
            v-if="step < 3"
            size="small"
            type="primary"
            :disabled="step === 2 && render === null"
            @click="next"
          >
            Next
          </NButton>
          <NButton
            v-else-if="created === null"
            size="small"
            type="primary"
            :loading="creating"
            @click="handleCreate"
          >
            Create service
          </NButton>
          <NButton
            v-else-if="!deployed"
            size="small"
            type="primary"
            :loading="deploying"
            @click="handleDeploy"
          >
            Deploy now
          </NButton>
        </div>
      </div>
    </NSpin>
  </NModal>
</template>

<style scoped>
.wizard {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

.wizard__step {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.wizard__desc {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.wizard__metaform {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 var(--space-4);
}

.wizard__preview {
  margin: 0;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  font-size: var(--text-xs);
  line-height: 1.6;
  color: var(--fg);
  overflow: auto;
  max-height: 40vh;
  white-space: pre-wrap;
  word-break: break-word;
}

.wizard__success {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.wizard__success h4 {
  font-size: var(--text-base);
}

.wizard__stub {
  border-left: 4px solid var(--warn);
  background: var(--surface);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
}

.wizard__stub h5 {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.wizard__stub p {
  margin: var(--space-1) 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.wizard__foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  border-top: 1px solid var(--border);
  padding-top: var(--space-3);
  margin-top: var(--space-4);
}

.wizard__counter {
  font-size: var(--text-xs);
}

.wizard__spacer {
  flex: 1 1 auto;
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 720px) {
  .wizard__metaform {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
