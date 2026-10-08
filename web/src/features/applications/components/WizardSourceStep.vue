<script setup lang="ts">
import { NAlert, NButton, NFormItem, NInput, NSelect, NSpace } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import BuildArgsEditor from "@/features/applications/components/BuildArgsEditor.vue";
import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";
import { isLatestImageTag } from "@/features/applications/schemas/applications";

const wizard = useCreateWizardState();
const { form } = wizard;

const { t } = useI18n();

/** latestTagWarn flags a moving tag: redeploys follow it, pin a digest to freeze. */
const latestTagWarn = computed<boolean>(
  () => wizard.isImage.value && isLatestImageTag(form.imageRef),
);
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

    <div class="form-row" v-else-if="wizard.isDockerfile.value">
      <NFormItem :label="t('applications.wizard.dockerfileContent')">
        <NInput
          v-model:value="form.dockerfileContent"
          type="textarea"
          class="mono"
          :rows="12"
          placeholder="FROM alpine:3.20"
        />
        <span class="field-hint">{{ t("applications.wizard.dockerfileHint") }}</span>
      </NFormItem>

      <NFormItem :label="t('applications.wizard.buildArgs')">
        <BuildArgsEditor v-model="form.buildArgs" />
        <span class="field-hint">{{ t("applications.wizard.buildArgsHint") }}</span>
      </NFormItem>
    </div>

    <div class="form-row" v-else-if="wizard.isImage.value">
      <NFormItem :label="t('applications.wizard.imageRef')">
        <NInput
          v-model:value="form.imageRef"
          class="mono"
          placeholder="registry.example.com/team/app:1.2"
        />
        <span class="field-hint">{{ t("applications.wizard.imageRefHint") }}</span>
      </NFormItem>

      <NFormItem :label="t('applications.wizard.registryUsername')">
        <NInput
          v-model:value="form.registryUsername"
          class="mono"
          :placeholder="t('applications.wizard.registryUsernamePlaceholder')"
        />
        <span class="field-hint">{{ t("applications.wizard.registryUsernameHint") }}</span>
      </NFormItem>

      <NFormItem :label="t('applications.wizard.registryPassword')">
        <NInput
          v-model:value="form.registryPassword"
          type="password"
          show-password-on="click"
          :placeholder="t('applications.wizard.registryPasswordPlaceholder')"
        />
        <span class="field-hint">{{ t("applications.wizard.registryPasswordHint") }}</span>
      </NFormItem>

      <NAlert v-if="latestTagWarn" type="warning" :show-icon="true">
        {{ t("applications.wizard.imageLatestWarn") }}
      </NAlert>
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

    <div class="form-row" v-else-if="wizard.isPrivateRepo.value">
      <NFormItem :label="t('applications.privateGit.url')">
        <NInput
          v-model:value="form.privateCloneUrl"
          class="mono"
          placeholder="git@github.com:owner/repo.git"
        />
        <span class="field-hint">{{ t("applications.privateGit.urlHint") }}</span>
      </NFormItem>

      <NFormItem :label="t('applications.privateGit.auth')">
        <NSelect
          v-model:value="form.privateAuth"
          :options="[
            { label: t('applications.privateGit.authSsh'), value: 'ssh' },
            { label: t('applications.privateGit.authHttps'), value: 'https' },
          ]"
        />
        <span class="field-hint">{{
          form.privateAuth === "https"
            ? t("applications.privateGit.authHttpsHint")
            : t("applications.privateGit.authSshHint")
        }}</span>
      </NFormItem>

      <template v-if="form.privateAuth === 'https'">
        <NFormItem :label="t('applications.privateGit.username')">
          <NInput v-model:value="form.httpsUsername" class="mono" autocomplete="off" />
        </NFormItem>
        <NFormItem :label="t('applications.privateGit.token')">
          <NInput
            v-model:value="form.httpsToken"
            class="mono"
            type="password"
            show-password-on="click"
            autocomplete="off"
          />
          <span class="field-hint">{{ t("applications.privateGit.tokenHint") }}</span>
        </NFormItem>
      </template>
    </div>

    <NAlert v-else type="info" :show-icon="true">
      {{ t("applications.wizard.sourceUnavailable") }}
    </NAlert>

    <NAlert v-if="wizard.sourceError.value" type="warning" :show-icon="true">
      {{ wizard.sourceError.value }}
    </NAlert>

    <div class="form-row">
      <NFormItem v-if="!wizard.isDockerfile.value && !wizard.isImage.value" :label="t('applications.wizard.branch')">
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
