<script setup lang="ts">
import {
  NAlert,
  NButton,
  NFormItem,
  NModal,
  NRadio,
  NRadioGroup,
  NSelect,
  NSpace,
  NText,
} from "naive-ui";
import { provide, toRef } from "vue";

import type {
  CreatedDatabase,
} from "@/features/databases/api/databases";
import WizardConfigureStep from "@/features/databases/components/WizardConfigureStep.vue";
import WizardReviewStep from "@/features/databases/components/WizardReviewStep.vue";
import {
  useCreateDatabaseWizard,
  wizardFormKey,
  wizardStepNames,
} from "@/features/databases/composables/useCreateDatabaseWizard";
import { ENGINES } from "@/features/databases/utils/databaseEngines";

interface Props {
  show: boolean;
}

interface Emits {
  "update:show": [value: boolean];
  created: [created: CreatedDatabase];
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const wizard = useCreateDatabaseWizard({
  show: toRef(props, "show"),
  onCreated: (created) => emit("created", created),
  onUpdateShow: (value) => emit("update:show", value),
});

provide(wizardFormKey, wizard.form);
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    title="Create database"
    style="width: 640px; max-width: 94vw"
    :mask-closable="false"
    class="form-container"
    @update:show="wizard.handleClose"
  >
    <NSpace vertical :size="16">
      <NText depth="3">
        Step {{ wizard.step.value + 1 }} of {{ wizardStepNames.length }} · {{ wizardStepNames[wizard.step.value] }}.
        Each database is a container with its own volume on one node; the
        credentials are generated server-side and stored encrypted.
      </NText>

      <NAlert v-if="wizard.errorMessage.value" type="error" :show-icon="true">
        {{ wizard.errorMessage.value }}
      </NAlert>

      <template v-if="wizard.step.value === 0">
        <NFormItem label="Engine" :show-feedback="false">
          <NRadioGroup v-model:value="wizard.form.engine">
            <NSpace vertical :size="8">
              <NRadio
                v-for="engine in ENGINES"
                :key="engine.value"
                :value="engine.value"
              >
                {{ engine.label }}
                <NText depth="3" class="mono">
                  · {{ engine.repo }}:{{ engine.defaultVersion }} · port
                  {{ engine.port }}
                </NText>
              </NRadio>
            </NSpace>
          </NRadioGroup>
        </NFormItem>
        <div class="form-row">
          <NFormItem label="Version" :show-feedback="false">
            <NSelect
              v-model:value="wizard.form.version"
              :options="wizard.versionOptions.value"
              :placeholder="`Default: ${wizard.selectedEngine.value.defaultVersion}`"
              clearable
            />
          </NFormItem>
          <NFormItem
            label="Node"
            feedback="The container and its volume live on this node."
          >
            <NSelect
              v-model:value="wizard.form.serverId"
              :options="wizard.serverOptions.value"
              placeholder="Select a node"
              :loading="wizard.serversStore.loading"
            />
          </NFormItem>
        </div>
      </template>

      <WizardConfigureStep v-else-if="wizard.step.value === 1" />

      <WizardReviewStep
        v-else
        :form="wizard.form"
        :image-preview="wizard.imagePreview.value"
        :server-label="wizard.serverLabel.value"
        :engine-port="wizard.selectedEngine.value.port"
        :created="wizard.created.value"
        :credential-rows="wizard.created.value === null ? [] : wizard.credentialRows(wizard.created.value.credentials)"
        @copy="(value, label) => void wizard.copyText(value, label)"
      />

      <NSpace justify="end" :size="8">
        <NButton :disabled="wizard.step.value === 0 || wizard.submitting.value" @click="wizard.goBack">
          Back
        </NButton>
        <NButton
          v-if="wizard.created.value === null"
          type="primary"
          :disabled="!wizard.canContinue.value"
          :loading="wizard.submitting.value"
          @click="wizard.goNext"
        >
          {{ wizard.step.value === wizardStepNames.length - 1 ? "Create database" : "Continue" }}
        </NButton>
        <NButton v-else type="primary" @click="wizard.handleClose(false)">
          Done
        </NButton>
      </NSpace>
    </NSpace>
  </NModal>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
