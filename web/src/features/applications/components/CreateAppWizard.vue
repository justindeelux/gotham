<script setup lang="ts">
import { NAlert, NButton, NModal, NText } from "naive-ui";
import { toRef } from "vue";

import type { Application } from "@/features/applications/api/applications";
import WizardBuildPackStep from "@/features/applications/components/WizardBuildPackStep.vue";
import WizardEnvStep from "@/features/applications/components/WizardEnvStep.vue";
import WizardReviewStep from "@/features/applications/components/WizardReviewStep.vue";
import WizardRuntimeStep from "@/features/applications/components/WizardRuntimeStep.vue";
import WizardSourceStep from "@/features/applications/components/WizardSourceStep.vue";
import {
  provideCreateWizard,
  useCreateAppWizard,
} from "@/features/applications/composables/useCreateAppWizard";
import ResourceScopeSummary from "@/features/projects/components/ResourceScopeSummary.vue";

interface Props {  show: boolean;
  /** Project the application is created in (the route's, changeable). */
  projectId?: string;
  /** Environment the application is created in (the route's, changeable). */
  environmentId?: string;
}

const props = withDefaults(defineProps<Props>(), { projectId: "", environmentId: "" });
const emit = defineEmits<{
  "update:show": [value: boolean];
  created: [application: Application];
}>();

const wizard = useCreateAppWizard(toRef(props, "show"), emit, {
  projectId: toRef(props, "projectId"),
  environmentId: toRef(props, "environmentId"),
});
provideCreateWizard(wizard);
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="Create application"
    :mask-closable="false"
    class="wizard-modal"
    style="width: 880px; max-width: 96vw"
    @update:show="wizard.handleShowChange"
  >
    <NText depth="3">
      Pick a source, build pack, port and domain, then deploy. Source, build
      pack and runtime are fixed once created; environment variables, volumes
      and domains can be changed afterwards.
    </NText>

    <ResourceScopeSummary
      :project-id="wizard.form.projectId"
      :environment-id="wizard.form.environmentId"
      @update:project-id="(value) => (wizard.form.projectId = value)"
      @update:environment-id="(value) => (wizard.form.environmentId = value)"
    />

    <div class="wizard">
      <div class="wizard-rail">
        <ol>
          <li
            v-for="(label, index) in wizard.stepNames"
            :key="label"
            :class="{
              'is-active': wizard.step.value === index,
              'is-done': wizard.step.value > index,
            }"
          >
            <span class="idx">{{ index + 1 }}</span>{{ label }}
          </li>
        </ol>
        <p class="wizard-rail-note">
          The webhook is created automatically after the first deploy.
        </p>
      </div>

      <div class="wizard-main form-container">
        <div class="wizard-body">
          <NAlert v-if="wizard.errorMessage.value" type="error" :show-icon="true">
            {{ wizard.errorMessage.value }}
          </NAlert>

          <WizardSourceStep v-if="wizard.step.value === 0" />
          <WizardBuildPackStep v-else-if="wizard.step.value === 1" />
          <WizardRuntimeStep v-else-if="wizard.step.value === 2" />
          <WizardEnvStep v-else-if="wizard.step.value === 3" />
          <WizardReviewStep v-else />
        </div>

        <div class="wizard-foot">
          <NButton v-if="wizard.step.value > 0" tertiary @click="wizard.step.value -= 1">Back</NButton>
          <span class="step-counter">Step {{ wizard.step.value + 1 }} / 5</span>
          <span class="grow" />
          <template v-if="wizard.step.value < 4">
            <NButton @click="wizard.closeWizard">Cancel</NButton>
            <NButton type="primary" :disabled="!wizard.canContinue.value" @click="wizard.step.value += 1">
              Continue
            </NButton>
          </template>
          <template v-else>
            <NButton @click="wizard.closeWizard">Cancel</NButton>
            <NButton
              type="primary"
              :loading="wizard.submitting.value"
              :disabled="!wizard.sourceValid.value || !wizard.runtimeValid.value || !wizard.scopeValid.value"
              @click="wizard.handleSubmit"
            >
              Create &amp; deploy
            </NButton>
          </template>
        </div>
      </div>
    </div>
  </NModal>
</template>

<style scoped>
.wizard {
  display: grid;
  grid-template-columns: 208px minmax(0, 1fr);
  min-height: 420px;
  margin-top: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
}

.wizard-rail {
  border-right: 1px solid var(--border);
  padding: var(--space-4) var(--space-3);
  background: color-mix(in oklab, var(--surface) 70%, var(--surface-warm));
}

.wizard-rail ol {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.wizard-rail li {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 7px var(--space-2);
  border-radius: var(--radius-sm);
  font-size: var(--text-sm);
  color: var(--muted);
}

.wizard-rail li .idx {
  width: 20px;
  height: 20px;
  border-radius: var(--radius-pill);
  border: 1.5px solid var(--meta);
  display: grid;
  place-items: center;
  font-family: var(--font-mono);
  font-size: 10px;
  flex: 0 0 auto;
}

.wizard-rail li.is-done {
  color: var(--fg);
}

.wizard-rail li.is-done .idx {
  background: var(--success);
  border-color: var(--success);
  color: var(--accent-on);
}

.wizard-rail li.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
  font-weight: 600;
}

.wizard-rail li.is-active .idx {
  border-color: var(--accent);
  color: var(--accent-ink);
}

.wizard-rail-note {
  margin: var(--space-4) 0 0;
  font-size: var(--text-xs);
  color: var(--meta);
  line-height: var(--leading-body);
}

.wizard-main {
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
}

.wizard-body {
  padding: var(--space-4);
  overflow-y: auto;
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.wizard-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border);
  flex: 0 0 auto;
}

.step-counter {
  font-size: var(--text-xs);
  color: var(--muted);
  font-family: var(--font-mono);
}

.grow {
  flex: 1;
  min-width: 0;
}

.wizard-modal :deep(.n-card-content) {
  max-height: 72vh;
  overflow-y: auto;
}

@media (max-width: 720px) {
  .wizard {
    grid-template-columns: minmax(0, 1fr);
  }

  .wizard-rail {
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }

  .wizard-rail ol {
    flex-direction: row;
    flex-wrap: wrap;
  }
}
</style>
