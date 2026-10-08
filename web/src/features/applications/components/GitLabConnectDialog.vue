<script setup lang="ts">
import { NAlert, NButton, NInput, NModal, NSpace } from "naive-ui";
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
    class="dialog-card"
    :mask-closable="false"
    @update:show="(value: boolean) => { if (!value) close(); }"
  >
    <NSpace vertical :size="16">
      <div class="form-row">
        <label class="field-label" for="gitlab-instance-input">{{ t("applications.gitSources.gitlabInstance") }}</label>
        <NInput
          v-model:value="baseUrl"
          :input-props="{ id: 'gitlab-instance-input' }"
          class="mono"
          placeholder="https://gitlab.com"
        />
        <span class="field-hint">{{ t("applications.gitSources.gitlabInstanceHint") }}</span>
      </div>

      <template v-if="!manual">
        <div class="form-row">
          <label class="field-label" for="gitlab-admin-token-input">{{ t("applications.gitSources.gitlabAdminToken") }}</label>
          <NInput
            v-model:value="adminToken"
            :input-props="{ id: 'gitlab-admin-token-input' }"
            type="password"
            class="mono"
            autocomplete="off"
          />
          <span class="field-hint">{{ t("applications.gitSources.gitlabAdminTokenHint") }}</span>
        </div>
      </template>

      <template v-else>
        <NAlert type="info" :show-icon="true">{{ t("applications.gitSources.gitlabManualHint") }}</NAlert>
        <div class="form-row">
          <label class="field-label" for="gitlab-redirect-input">{{ t("applications.gitSources.gitlabRedirect") }}</label>
          <NInput :value="redirectUri" :input-props="{ id: 'gitlab-redirect-input' }" class="mono" readonly />
          <span class="field-hint">{{ t("applications.gitSources.gitlabRedirectHint") }}</span>
        </div>
        <div class="form-row">
          <label class="field-label" for="gitlab-client-id-input">{{ t("applications.gitSources.gitlabClientId") }}</label>
          <NInput v-model:value="clientId" :input-props="{ id: 'gitlab-client-id-input' }" class="mono" autocomplete="off" />
        </div>
        <div class="form-row">
          <label class="field-label" for="gitlab-client-secret-input">{{ t("applications.gitSources.gitlabClientSecret") }}</label>
          <NInput
            v-model:value="clientSecret"
            :input-props="{ id: 'gitlab-client-secret-input' }"
            type="password"
            class="mono"
            autocomplete="off"
          />
          <span class="field-hint">{{ t("applications.gitSources.gitlabClientSecretHint") }}</span>
        </div>
        <div class="form-row">
          <label class="field-label" for="gitlab-scopes-input">{{ t("applications.gitSources.gitlabScopes") }}</label>
          <NInput v-model:value="scopes" :input-props="{ id: 'gitlab-scopes-input' }" class="mono" />
        </div>
      </template>

      <NAlert v-if="savedReconnect" type="info" :show-icon="true">{{
        t("applications.gitSources.gitlabSavedReconnect")
      }}</NAlert>
      <NAlert v-if="loadError" type="error" :show-icon="true">{{ loadError }}</NAlert>

      <NSpace :size="12">
        <NButton type="primary" :disabled="!canSubmit" :loading="working" @click="void submit()">
          {{ manual ? t("applications.gitSources.gitlabStoreConnect") : t("applications.gitSources.gitlabProvision") }}
        </NButton>
        <NButton :disabled="working" @click="void toggleManual()">
          {{ manual ? t("applications.gitSources.gitlabAutoToggle") : t("applications.gitSources.gitlabManualToggle") }}
        </NButton>
        <NButton :disabled="working" @click="close">{{ t("applications.gitSources.cancel") }}</NButton>
      </NSpace>
    </NSpace>
  </NModal>
</template>

<style scoped>
.dialog-card {
  max-width: 560px;
}

.form-row {
  display: grid;
  gap: 4px;
}

.field-label {
  font-size: var(--text-xs);
  color: var(--meta);
}

.field-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
