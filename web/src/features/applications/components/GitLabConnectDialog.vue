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

/** canSubmit gates the active mode: a token for auto, id+secret for manual. */
const canSubmit = computed<boolean>(() => {
  if (!instanceValid.value || working.value) {
    return false;
  }
  if (manual.value) {
    return clientId.value.trim() !== "" && clientSecret.value !== "";
  }
  return adminToken.value !== "";
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
 * Secrets are cleared before leaving: the browser navigates away. */
async function submit(): Promise<void> {
  if (!canSubmit.value) {
    return;
  }
  working.value = true;
  rawError.value = null;
  try {
    if (manual.value) {
      await fns.connectGitLabManual({
        baseUrl: baseUrl.value.trim(),
        clientId: clientId.value.trim(),
        clientSecret: clientSecret.value,
        scopes: scopes.value.trim() === "" ? undefined : scopes.value.trim(),
      });
    } else {
      await fns.connectGitLabAuto(baseUrl.value.trim(), adminToken.value);
    }
    clearSecrets();
    emit("connected");
    emit("update:show", false);
  } catch (error) {
    rawError.value = error;
  } finally {
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
        <label class="field-label" for="gitlab-instance">{{ t("applications.gitSources.gitlabInstance") }}</label>
        <NInput
          id="gitlab-instance"
          v-model:value="baseUrl"
          class="mono"
          placeholder="https://gitlab.com"
        />
        <span class="field-hint">{{ t("applications.gitSources.gitlabInstanceHint") }}</span>
      </div>

      <template v-if="!manual">
        <div class="form-row">
          <label class="field-label" for="gitlab-admin-token">{{ t("applications.gitSources.gitlabAdminToken") }}</label>
          <NInput
            id="gitlab-admin-token"
            v-model:value="adminToken"
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
          <label class="field-label" for="gitlab-redirect">{{ t("applications.gitSources.gitlabRedirect") }}</label>
          <NInput id="gitlab-redirect" :value="redirectUri" class="mono" readonly />
          <span class="field-hint">{{ t("applications.gitSources.gitlabRedirectHint") }}</span>
        </div>
        <div class="form-row">
          <label class="field-label" for="gitlab-client-id">{{ t("applications.gitSources.gitlabClientId") }}</label>
          <NInput id="gitlab-client-id" v-model:value="clientId" class="mono" autocomplete="off" />
        </div>
        <div class="form-row">
          <label class="field-label" for="gitlab-client-secret">{{ t("applications.gitSources.gitlabClientSecret") }}</label>
          <NInput
            id="gitlab-client-secret"
            v-model:value="clientSecret"
            type="password"
            class="mono"
            autocomplete="off"
          />
          <span class="field-hint">{{ t("applications.gitSources.gitlabClientSecretHint") }}</span>
        </div>
        <div class="form-row">
          <label class="field-label" for="gitlab-scopes">{{ t("applications.gitSources.gitlabScopes") }}</label>
          <NInput id="gitlab-scopes" v-model:value="scopes" class="mono" />
        </div>
      </template>

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
