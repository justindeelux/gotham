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
import ResourceScopeSummary from "@/features/projects/components/ResourceScopeSummary.vue";
import ServerPicker from "@/features/projects/components/ServerPicker.vue";

interface Props {
  show: boolean;
  /** Project the database is created in (the route's, changeable). */
  projectId?: string;
  /** Environment the database is created in (the route's, changeable). */
  environmentId?: string;
}

interface Emits {
  "update:show": [value: boolean];
  created: [created: CreatedDatabase];
}

const props = withDefaults(defineProps<Props>(), { projectId: "", environmentId: "" });
const emit = defineEmits<Emits>();

const wizard = useCreateDatabaseWizard({
  show: toRef(props, "show"),
  onCreated: (created) => emit("created", created),
  onUpdateShow: (value) => emit("update:show", value),
  projectId: toRef(props, "projectId"),
  environmentId: toRef(props, "environmentId"),
});

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

provide(wizardFormKey, wizard.form);

</script>

<template>
  <NModal
    :show="show"
    preset="card"
    :title="t('databases.wizard.title')"
    style="width: 640px; max-width: 94vw"
    :mask-closable="false"
    class="form-container"
    @update:show="wizard.handleClose"
  >
    <NSpace vertical :size="16">
      <NText depth="3">
        {{
          t("databases.wizard.stepOf", {
            current: wizard.step.value + 1,
            total: wizardStepNames.length,
            step: wizard.stepNames.value[wizard.step.value],
          })
        }}
        {{ t("databases.wizard.intro") }}
      </NText>

      <NAlert v-if="wizard.errorMessage.value" type="error" :show-icon="true">
        {{ wizard.errorMessage.value }}
      </NAlert>

      <ResourceScopeSummary
        :project-id="wizard.form.projectId"
        :environment-id="wizard.form.environmentId"
        @update:project-id="(value) => (wizard.form.projectId = value)"
        @update:environment-id="(value) => (wizard.form.environmentId = value)"
      />

      <template v-if="wizard.step.value === 0">
        <NFormItem :label="t('databases.wizard.engineStep.engine')" :show-feedback="false">
          <NRadioGroup v-model:value="wizard.form.engine">
            <NSpace vertical :size="8">
              <NRadio
                v-for="engine in ENGINES"
                :key="engine.value"
                :value="engine.value"
              >
                {{ engine.label }}
                <NText depth="3" class="mono">
                  {{
                    t("databases.wizard.engineStep.engineMeta", {
                      repo: engine.repo,
                      version: engine.defaultVersion,
                      port: engine.port,
                    })
                  }}
                </NText>
              </NRadio>
            </NSpace>
          </NRadioGroup>
        </NFormItem>
        <div class="form-row">
          <NFormItem :label="t('databases.wizard.engineStep.version')" :show-feedback="false">
            <NSelect
              v-model:value="wizard.form.version"
              :options="wizard.versionOptions.value"
              :placeholder="t('databases.wizard.engineStep.defaultVersion', { version: wizard.selectedEngine.value.defaultVersion })"
              clearable
            />
          </NFormItem>
          <ServerPicker v-model="wizard.form.serverId" :label="t('databases.wizard.engineStep.node')" />
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
          {{ t("databases.wizard.back") }}
        </NButton>
        <NButton
          v-if="wizard.created.value === null"
          type="primary"
          :disabled="!wizard.canContinue.value"
          :loading="wizard.submitting.value"
          @click="wizard.goNext"
        >
          {{ wizard.step.value === wizardStepNames.length - 1 ? t("databases.wizard.create") : t("databases.wizard.continue") }}
        </NButton>
        <NButton v-else type="primary" @click="wizard.handleClose(false)">
          {{ t("databases.wizard.done") }}
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
