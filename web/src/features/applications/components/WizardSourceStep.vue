<script setup lang="ts">
import { NAlert, NButton, NFormItem, NInput, NRadio, NRadioGroup, NSelect, NSpace } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";

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
      <NFormItem
        v-if="!wizard.sourceTypeLocked.value"
        :label="t('applications.wizard.sourceType')"
        :show-feedback="true"
      >
        <NSelect
          v-model:value="form.sourceType"
          :options="wizard.sourceTypeOptions.value"
          :placeholder="t('applications.wizard.sourceTypePlaceholder')"
        />
        <span class="field-hint">{{ t("applications.wizard.sourceTypeHint") }}</span>
      </NFormItem>
      <!-- Preselected by the picker: read-only summary, Change goes back to the picker. -->
      <div v-else class="preselected">
        <span>{{ t("applications.wizard.sourcePreselected", { source: wizard.lockedSourceLabel.value }) }}</span>
        <NButton size="small" quaternary @click="wizard.closeWizard()">
          {{ t("applications.wizard.sourceChange") }}
        </NButton>
      </div>
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

    <template v-else-if="wizard.isDockerfile.value">
      <div class="form-row">
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
      </div>

      <div class="form-row">
        <NFormItem :label="t('applications.wizard.buildArgs')">
          <BuildArgsEditor v-model="form.buildArgs" />
          <span class="field-hint">{{ t("applications.wizard.buildArgsHint") }}</span>
        </NFormItem>
      </div>
    </template>

    <template v-else-if="wizard.isImage.value">
      <div class="form-row">
        <NFormItem :label="t('applications.wizard.imageRef')">
          <NInput
            v-model:value="form.imageRef"
            class="mono"
            placeholder="registry.example.com/team/app:1.2"
          />
          <span class="field-hint">{{ t("applications.wizard.imageRefHint") }}</span>
          <NAlert v-if="latestTagWarn" class="field-alert" type="warning" :show-icon="false">
            {{ t("applications.wizard.imageLatestWarn") }}
          </NAlert>
        </NFormItem>
      </div>

      <div class="form-row">
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
      </div>
    </template>

    <div class="form-row" v-else-if="wizard.isProviderFlow.value">
      <NAlert
        v-if="wizard.providerOptions.value.length === 0"
        type="info"
        :show-icon="true"
        style="margin-bottom: 8px"
      >
        <NSpace align="center" :size="8">
          <span>{{ t("applications.gitSources.wizardConnectHint") }}</span>
          <RouterLink :to="{ name: 'git-sources' }">
            {{ t("applications.gitSources.wizardConnect") }}
          </RouterLink>
        </NSpace>
      </NAlert>
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

    <template v-else-if="wizard.isCompose.value">
      <div class="form-row">
        <NFormItem :label="t('applications.wizard.composeMode')">
          <NRadioGroup v-model:value="form.composeMode">
            <NSpace :size="12">
              <NRadio
                v-for="mode in wizard.composeModeOptions.value"
                :key="mode.value"
                :value="mode.value"
              >
                {{ mode.label }}
              </NRadio>
            </NSpace>
          </NRadioGroup>
          <span class="field-hint">{{ t("applications.wizard.composeModeHint") }}</span>
        </NFormItem>
      </div>

      <template v-if="wizard.isComposePaste.value">
        <div class="form-row">
          <NFormItem :label="t('applications.wizard.composeContent')">
            <NInput
              v-model:value="form.composeContent"
              type="textarea"
              class="mono"
              :rows="12"
              placeholder="services:"
            />
            <span class="field-hint">{{ t("applications.wizard.composeContentHint") }}</span>
            <NAlert class="field-alert" type="warning" :show-icon="false">
              {{ t("applications.wizard.composeSecretsHint") }}
            </NAlert>
          </NFormItem>
        </div>
      </template>

      <template v-else>
        <div class="form-row">
          <NFormItem :label="t('applications.wizard.provider')">
            <NSelect
              v-model:value="form.providerId"
              :options="wizard.providerOptions.value"
              :loading="wizard.providersStore.loading"
              :placeholder="t('applications.wizard.providerPlaceholder')"
              clearable
            />
            <span class="field-hint">{{ t("applications.wizard.composeRepoProviderHint") }}</span>
            <NAlert
              v-if="wizard.providersStore.reposError"
              type="error"
              :show-icon="true"
              style="margin-top: 8px"
            >
              <NSpace align="center" :size="12" wrap>
                <span>{{ wizard.providersStore.reposError }}</span>
                <NButton size="small" @click="void wizard.loadRepos()">{{ t("common.actions.retry") }}</NButton>
              </NSpace>
            </NAlert>
          </NFormItem>
        </div>

        <div class="form-row">
          <NFormItem v-if="form.providerId !== ''" :label="t('applications.wizard.repository')">
            <NSelect
              v-model:value="form.repoFullName"
              :options="wizard.repoOptions.value"
              :loading="wizard.providersStore.reposLoading"
              :disabled="form.providerId === ''"
              :placeholder="t('applications.wizard.repositoryPlaceholder')"
              filterable
              @update:value="wizard.handleRepoSelect"
            />
            <span class="field-hint">{{ t("applications.wizard.repoHint") }}</span>
          </NFormItem>

          <NFormItem v-else :label="t('applications.wizard.cloneUrl')">
            <NInput
              v-model:value="form.publicCloneUrl"
              class="mono"
              placeholder="https://github.com/owner/repo.git"
            />
            <span class="field-hint">{{ t("applications.wizard.cloneHint") }}</span>
          </NFormItem>

          <NFormItem :label="t('applications.wizard.composeFile')">
            <NInput
              v-model:value="form.composeFile"
              class="mono"
              placeholder="docker-compose.yml"
            />
            <span class="field-hint">{{ t("applications.wizard.composeFileHint") }}</span>
          </NFormItem>
        </div>
      </template>

      <div class="form-row">
        <NFormItem :label="t('applications.wizard.composeService')">
          <NSelect
            v-model:value="form.composeService"
            :options="wizard.composeServiceOptions.value"
            :placeholder="t('applications.wizard.composeServicePlaceholder')"
            filterable
            tag
          />
          <span class="field-hint">{{ t("applications.wizard.composeServiceHint") }}</span>
        </NFormItem>
      </div>
    </template>

    <NAlert v-else type="info" :show-icon="true">
      {{ t("applications.wizard.sourceUnavailable") }}
    </NAlert>

    <NAlert v-if="wizard.sourceError.value" type="warning" :show-icon="true">
      {{ wizard.sourceError.value }}
    </NAlert>

    <div class="form-row">
      <NFormItem v-if="!wizard.isDockerfile.value && !wizard.isImage.value && !wizard.isComposePaste.value" :label="t('applications.wizard.branch')">
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

/* Inline field notices sit on their own line below the input + hint inside
 * Naive's wrapping .n-form-item-blank row. NAlert has no size prop, so the
 * compact inline look is a smaller font and tighter padding. */
.field-alert {
  flex-basis: 100%;
  margin-top: var(--space-2);
  padding: 6px 10px;
  font-size: var(--text-xs);
}

.preselected {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.mono {
  font-family: var(--font-mono);
}
</style>
