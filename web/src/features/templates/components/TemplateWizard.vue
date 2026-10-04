<script setup lang="ts">
import {
  NAlert,
  NModal,
  NSpin,
  NStep,
  NSteps,
} from "naive-ui";
import { toRef } from "vue";

import { useTemplateWizard } from "@/features/templates/composables/useTemplateWizard";
import WizardConfigureStep from "./WizardConfigureStep.vue";
import WizardCreatedPanel from "./WizardCreatedPanel.vue";
import WizardFooter from "./WizardFooter.vue";
import WizardPreviewStep from "./WizardPreviewStep.vue";
import WizardTargetForm from "./WizardTargetForm.vue";

interface Props {
  show: boolean;
  slug: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const wizard = useTemplateWizard(toRef(props, "show"), toRef(props, "slug"));

function close(): void {
  emit("update:show", false);
}
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    class="template-wizard"
    style="width: 780px; max-width: 96vw"
    :closable="!wizard.creating.value && !wizard.deploying.value"
    :mask-closable="!wizard.creating.value && !wizard.deploying.value"
    @update:show="(value: boolean) => emit('update:show', value)"
    @after-leave="wizard.reset"
  >
    <template #header>
      Deploy template {{ wizard.detail.value?.name ?? slug }}
    </template>

    <NSpin :show="wizard.detailLoading.value">
      <NAlert v-if="wizard.detailError.value" type="error" :show-icon="true">
        {{ wizard.detailError.value }}
      </NAlert>

      <div v-else-if="wizard.detail.value" class="wizard">
        <NSteps :current="wizard.step.value" size="small">
          <NStep title="Configure" description="Fill the template fields" />
          <NStep title="Compose preview" description="Rendered from the schema" />
          <NStep title="Create" description="Name the service and pick a node" />
        </NSteps>

        <WizardConfigureStep
          v-if="wizard.step.value === 1"
          :slug="wizard.detail.value.slug"
          :fields="wizard.fields.value"
          :values="wizard.values.value"
          :errors="wizard.formErrors.value"
          @update:values="(next) => (wizard.values.value = next)"
        />

        <WizardPreviewStep
          v-else-if="wizard.step.value === 2"
          :render-loading="wizard.renderLoading.value"
          :render-error="wizard.renderError.value"
          :render="wizard.render.value"
          :secret-keys="wizard.secretKeys.value"
        />

        <section v-else class="wizard__step" data-testid="wizard-step-3">
          <WizardTargetForm
            v-if="wizard.created.value === null"
            :name="wizard.name.value"
            :server-id="wizard.serverId.value"
            :name-error="wizard.nameError.value"
            :node-error="wizard.nodeError.value"
            :create-error="wizard.createError.value"
            @update:name="(next) => (wizard.name.value = next)"
            @update:server-id="(next) => (wizard.serverId.value = next)"
          />

          <WizardCreatedPanel
            v-else
            :created="wizard.created.value"
            :deployed="wizard.deployed.value"
            :deploy-error="wizard.deployError.value"
            :log-services="wizard.render.value?.spec.services ?? []"
            :log-title="`${wizard.created.value.name} logs`"
            @close="close"
          />
        </section>

        <WizardFooter
          :step="wizard.step.value"
          :render-loading="wizard.renderLoading.value"
          :has-render="wizard.render.value !== null"
          :created="wizard.created.value !== null"
          :creating="wizard.creating.value"
          :deploying="wizard.deploying.value"
          :deployed="wizard.deployed.value"
          @back="wizard.step.value -= 1"
          @next="wizard.next()"
          @close="close"
          @create="wizard.handleCreate()"
          @deploy="wizard.handleDeploy()"
        />
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
</style>
