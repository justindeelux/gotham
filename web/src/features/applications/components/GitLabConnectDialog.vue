<script setup lang="ts">
import { NAlert, NButton, NForm, NFormItem, NInput, NModal, NSpace } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeProviderError } from "@/features/applications/api/providers";
import { useGitSourcesPage } from "@/features/applications/composables/useGitSourcesPage";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{
  (_event: "update:show", _value: boolean): void;
  (_event: "connected"): void;
}>();

const { t } = useI18n();
const fns = useGitSourcesPage();

const manual = ref(false);
const baseUrl = ref("https://gitlab.com");
const adminToken = ref("");
const clientId = ref("");
const clientSecret = ref("");
const scopes = ref("");
const redirectUri = ref("");
const working = ref(false);
const rawError = ref<unknown>(null);
/** savedReconnect flags a provision/store that succeeded while the PKCE
 * start that follows failed: the row exists with a Reconnect action. */
const savedReconnect = ref(false);
const loadError = computed<string>(() =>
  rawError.value === null ? "" : describeProviderError(rawError.value),
);

/** instanceValid gates submit on an http(s) URL with a host. */
const instanceValid = computed<boolean>(() => {
  try {
    const parsed = new URL(baseUrl.value.trim());
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.hostname !== "";
  } catch {
    return false;
  }
});

/** canSubmit gates the active mode: a token for auto, id+secret+redirect
 * for manual. The redirect URI comes from setup-info: without it the manual
 * OAuth app cannot be registered, so submit stays closed (fail-closed). */
const canSubmit = computed<boolean>(() => {
  if (!instanceValid.value || working.value) {
    return false;
  }
  if (manual.value) {
    return (
      clientId.value.trim() !== "" &&
      clientSecret.value !== "" &&
      redirectUri.value.trim() !== ""
    );
  }
  return adminToken.value.trim() !== "";
});

/** clearSecrets drops every write-only field, so nothing survives the dialog. */
function clearSecrets(): void {
  adminToken.value = "";
  clientSecret.value = "";
}

function close(): void {
  clearSecrets();
  rawError.value = null;
  emit("update:show", false);
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      manual.value = false;
      baseUrl.value = "https://gitlab.com";
      clientId.value = "";
      scopes.value = "";
      redirectUri.value = "";
      clearSecrets();
      rawError.value = null;
      savedReconnect.value = false;
    }
  },
);

/** toggleManual switches modes and loads the manual-application details. */
async function toggleManual(): Promise<void> {
  manual.value = !manual.value;
  rawError.value = null;
  if (manual.value && instanceValid.value && redirectUri.value === "") {
    working.value = true;
    try {
      const info = await fns.gitLabManualInfo(baseUrl.value.trim());
      redirectUri.value = info.redirect_uri;
      scopes.value = info.scopes;
    } catch (error) {
      rawError.value = error;
    } finally {
      working.value = false;
    }
  }
}

/** submit provisions (auto) or stores (manual), then starts the PKCE sign-in.
 * Secrets clear on every attempt, success or failure: the admin token is
 * consumed by the single provisioning call either way. When the store
 * succeeded but the sign-in start failed, the dialog names the Reconnect
 * action instead of only the raw error. */
async function submit(): Promise<void> {
  if (!canSubmit.value) {
    return;
  }
  working.value = true;
  rawError.value = null;
  savedReconnect.value = false;
  try {
    const created = manual.value
      ? await fns.provisionGitLabManual({
          baseUrl: baseUrl.value.trim(),
          clientId: clientId.value.trim(),
          clientSecret: clientSecret.value,
          scopes: scopes.value.trim() === "" ? undefined : scopes.value.trim(),
        })
      : await fns.provisionGitLabAuto(baseUrl.value.trim(), adminToken.value);
    try {
      await fns.startAuthorize(created.id);
    } catch (error) {
      savedReconnect.value = true;
      rawError.value = error;
      return;
    }
    emit("connected");
    emit("update:show", false);
  } catch (error) {
    rawError.value = error;
  } finally {
    clearSecrets();
    working.value = false;
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="t('applications.gitSources.gitlabTitle')"
    class="dialog-card connect-modal"
    style="width: 520px; max-width: 94vw"
    :mask-closable="false"
    @update:show="(value: boolean) => { if (!value) close(); }"
  >
    <NForm label-placement="top">
      <NFormItem
        :label="t('applications.gitSources.gitlabInstance')"
        :label-props="{ for: 'gitlab-instance-input' }"
      >
        <NInput
          v-model:value="baseUrl"
          :input-props="{ id: 'gitlab-instance-input' }"
          class="mono"
          placeholder="https://gitlab.com"
        />
        <template #feedback>
          <span class="field-hint">{{ t("applications.gitSources.gitlabInstanceHint") }}</span>
        </template>
      </NFormItem>

      <template v-if="!manual">
        <NFormItem
          :label="t('applications.gitSources.gitlabAdminToken')"
          :label-props="{ for: 'gitlab-admin-token-input' }"
        >
          <NInput
            v-model:value="adminToken"
            :input-props="{ id: 'gitlab-admin-token-input' }"
            type="password"
            class="mono"
            autocomplete="off"
            :placeholder="t('applications.gitSources.gitlabAdminTokenPlaceholder')"
          />
          <template #feedback>
            <span class="field-hint">{{ t("applications.gitSources.gitlabAdminTokenHint") }}</span>
          </template>
        </NFormItem>
      </template>

      <template v-else>
        <NAlert type="info" :show-icon="true">{{ t("applications.gitSources.gitlabManualHint") }}</NAlert>
        <NFormItem
          :label="t('applications.gitSources.gitlabRedirect')"
          :label-props="{ for: 'gitlab-redirect-input' }"
        >
          <NInput :value="redirectUri" :input-props="{ id: 'gitlab-redirect-input' }" class="mono" readonly />
          <template #feedback>
            <span class="field-hint">{{ t("applications.gitSources.gitlabRedirectHint") }}</span>
          </template>
        </NFormItem>
        <NFormItem
          :label="t('applications.gitSources.gitlabClientId')"
          :label-props="{ for: 'gitlab-client-id-input' }"
        >
          <NInput v-model:value="clientId" :input-props="{ id: 'gitlab-client-id-input' }" class="mono" autocomplete="off" />
        </NFormItem>
        <NFormItem
          :label="t('applications.gitSources.gitlabClientSecret')"
          :label-props="{ for: 'gitlab-client-secret-input' }"
        >
          <NInput
            v-model:value="clientSecret"
            :input-props="{ id: 'gitlab-client-secret-input' }"
            type="password"
            class="mono"
            autocomplete="off"
          />
          <template #feedback>
            <span class="field-hint">{{ t("applications.gitSources.gitlabClientSecretHint") }}</span>
          </template>
        </NFormItem>
        <NFormItem
          :label="t('applications.gitSources.gitlabScopes')"
          :label-props="{ for: 'gitlab-scopes-input' }"
        >
          <NInput v-model:value="scopes" :input-props="{ id: 'gitlab-scopes-input' }" class="mono" />
        </NFormItem>
      </template>

      <NAlert v-if="savedReconnect" type="info" :show-icon="true">{{
        t("applications.gitSources.gitlabSavedReconnect")
      }}</NAlert>
      <NAlert v-if="loadError" type="error" :show-icon="true">{{ loadError }}</NAlert>
    </NForm>
    <template #footer>
      <div class="connect-footer">
        <NButton quaternary :disabled="working" @click="void toggleManual()">
          {{ manual ? t("applications.gitSources.gitlabAutoToggle") : t("applications.gitSources.gitlabManualToggle") }}
        </NButton>
        <NSpace :size="8">
          <NButton :disabled="working" @click="close">{{ t("applications.gitSources.cancel") }}</NButton>
          <NButton type="primary" :disabled="!canSubmit" :loading="working" @click="void submit()">
            {{ manual ? t("applications.gitSources.gitlabStoreConnect") : t("applications.gitSources.gitlabProvision") }}
          </NButton>
        </NSpace>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.connect-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.mono {
  font-family: var(--font-mono);
}
</style>

<!--
  Unscoped on purpose: NModal teleports the card to <body>, so scoped
  selectors (which compile to a [data-v] ancestor match) never reach it.
  Every rule stays behind the .connect-modal class owned by this dialog.
-->
<style>
.connect-modal.n-modal.n-card {
  max-height: calc(100vh - 64px);
  display: flex;
  flex-direction: column;
}

.connect-modal.n-modal.n-card > .n-card-content {
  overflow-y: auto;
  min-height: 0;
}

.connect-modal .n-form-item-blank {
  display: block;
}

.connect-modal .n-form-item-blank > .n-input {
  width: 100%;
}
</style>
