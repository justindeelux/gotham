<script setup lang="ts">
import { NAlert, NButton, NFormItem, NInput, NSelect, NSpace } from "naive-ui";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;
</script>

<template>
  <NSpace vertical :size="16">
    <div class="form-row">
      <NFormItem label="Provider" :show-feedback="true">
        <NSelect
          v-model:value="form.providerId"
          :options="wizard.providerOptions.value"
          :loading="wizard.providersStore.loading"
          placeholder="Select a connected provider"
        />
        <span class="field-hint">Each provider uses its own OAuth app.</span>
      </NFormItem>

      <NFormItem v-if="wizard.isPublicRepo.value" label="Clone URL">
        <NInput
          v-model:value="form.publicCloneUrl"
          class="mono"
          placeholder="https://github.com/owner/repo.git"
        />
        <span class="field-hint">Any public repo — no provider connection needed.</span>
      </NFormItem>

      <NFormItem v-else label="Repository">
        <NSelect
          v-model:value="form.repoFullName"
          :options="wizard.repoOptions.value"
          :loading="wizard.providersStore.reposLoading"
          :disabled="form.providerId === ''"
          placeholder="Select a repository"
          filterable
          @update:value="wizard.handleRepoSelect"
        />
        <span class="field-hint">Private repos deploy with an SSH deploy key.</span>
        <NAlert
          v-if="wizard.providersStore.reposError"
          type="error"
          :show-icon="true"
          style="margin-top: 8px"
        >
          <NSpace align="center" :size="12" wrap>
            <span>{{ wizard.providersStore.reposError }}</span>
            <NButton size="small" @click="void wizard.loadRepos()">Retry</NButton>
          </NSpace>
        </NAlert>
      </NFormItem>
    </div>

    <NAlert v-if="wizard.sourceError.value" type="warning" :show-icon="true">
      {{ wizard.sourceError.value }}
    </NAlert>

    <div class="form-row">
      <NFormItem label="Branch">
        <NInput v-model:value="form.branch" class="mono" placeholder="main" />
        <span class="field-hint">Branch listing is not exposed by the API yet — the default branch is prefilled.</span>
      </NFormItem>
      <NFormItem label="Application name">
        <NInput v-model:value="form.name" class="mono" placeholder="storefront" />
        <span class="field-hint">Lowercase, digits and dashes (3-31 chars). Used for the container and image tag.</span>
      </NFormItem>
    </div>
  </NSpace>
</template>

<style scoped>
.field-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
