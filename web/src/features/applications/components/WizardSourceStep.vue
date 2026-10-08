<script setup lang="ts">
import { NAlert, NButton, NFormItem, NInput, NSelect, NSpace } from "naive-ui";
import { useI18n } from "vue-i18n";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;

const { t } = useI18n();
</script>

<template>
  <NSpace vertical :size="16">
    <div class="form-row">
      <NFormItem :label="t('applications.wizard.sourceType')" :show-feedback="true">
        <NSelect
          v-model:value="form.sourceType"
          :options="wizard.sourceTypeOptions.value"
          :placeholder="t('applications.wizard.sourceTypePlaceholder')"
        />
        <span class="field-hint">{{ t("applications.wizard.sourceTypeHint") }}</span>
      </NFormItem>
    </div>

    <div class="form-row" v-if="wizard.isPublicRepo.value">
      <NFormItem :label="t('applications.wizard.cloneUrl')">
        <NInput
          v-model:value="form.publicCloneUrl"
          class="mono"
          placeholder="https://github.com/owner/repo.git"
        />
        <span class="field-hint">{{ t("applications.wizard.cloneHint") }}</span>
      </NFormItem>
    </div>

    <div class="form-row" v-else-if="wizard.isProviderFlow.value">
      <NFormItem :label="t('applications.wizard.provider')" :show-feedback="true">
        <NSelect
          v-model:value="form.providerId"
          :options="wizard.providerOptions.value"
          :loading="wizard.isGitHubAppFlow.value ? wizard.githubAppStore.loading : wizard.providersStore.loading"
          :placeholder="t('applications.wizard.providerPlaceholder')"
        />
        <span class="field-hint">{{ t("applications.wizard.providerHint") }}</span>
      </NFormItem>

      <NFormItem :label="t('applications.wizard.repository')">
        <NSelect
          v-model:value="form.repoFullName"
          :options="wizard.repoOptions.value"
          :loading="wizard.isGitHubAppFlow.value ? wizard.githubAppStore.reposLoading : wizard.providersStore.reposLoading"
          :disabled="form.providerId === ''"
          :placeholder="t('applications.wizard.repositoryPlaceholder')"
          filterable
          @update:value="wizard.handleRepoSelect"
        />
        <span class="field-hint">{{ t("applications.wizard.repoHint") }}</span>
        <span v-if="wizard.reposTruncated.value" class="field-hint">{{
          t("applications.wizard.repoTruncatedHint")
        }}</span>
        <NAlert
          v-if="wizard.isGitHubAppFlow.value ? wizard.githubAppStore.reposError : wizard.providersStore.reposError"
          type="error"
          :show-icon="true"
          style="margin-top: 8px"
        >
          <NSpace align="center" :size="12" wrap>
            <span>{{ wizard.isGitHubAppFlow.value ? wizard.githubAppStore.reposError : wizard.providersStore.reposError }}</span>
            <NButton size="small" @click="void wizard.loadRepos()">{{ t("common.actions.retry") }}</NButton>
          </NSpace>
        </NAlert>
      </NFormItem>
    </div>

    <NAlert v-else type="info" :show-icon="true">
      {{ t("applications.wizard.sourceUnavailable") }}
    </NAlert>

    <NAlert v-if="wizard.sourceError.value" type="warning" :show-icon="true">
      {{ wizard.sourceError.value }}
    </NAlert>

    <div class="form-row">
      <NFormItem :label="t('applications.wizard.branch')">
        <NSelect
          v-if="wizard.branchOptions.value.length > 0"
          v-model:value="form.branch"
          :options="wizard.branchOptions.value"
          :loading="wizard.isGitHubAppFlow.value ? wizard.githubAppStore.branchesLoading : wizard.providersStore.branchesLoading"
          :placeholder="t('applications.wizard.branchPlaceholder')"
          filterable
          tag
        />
        <NInput v-else v-model:value="form.branch" class="mono" placeholder="main" />
        <span class="field-hint">{{
          wizard.isPublicRepo.value
            ? t("applications.wizard.branchAutoHint")
            : t("applications.wizard.branchHint")
        }}</span>
      </NFormItem>
      <NFormItem :label="t('applications.wizard.appName')">
        <NInput v-model:value="form.name" class="mono" placeholder="storefront" />
        <span class="field-hint">{{ t("applications.wizard.appNameHint") }}</span>
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
