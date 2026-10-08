<script setup lang="ts">
import { NAlert, NButton, NCard, NInput, NSpace } from "naive-ui";
import { useI18n } from "vue-i18n";
import { onMounted, ref } from "vue";

import {
  createDeployKey,
  describeApplicationError,
  getDeployKey,
  getGitCredential,
  setGitCredential,
  testConnection,
  type ConnectionResult,
  type DeployKey,
  type GitCredentialState,
} from "@/features/applications/api/applications";
import { useCopyText } from "@/shared/composables/useCopyText";

const props = defineProps<{
  applicationId: string;
}>();

const { t } = useI18n();
const { copyText } = useCopyText();

const key = ref<DeployKey | null>(null);
const keyLoading = ref(true);
const keySaving = ref(false);
const keyError = ref("");
const cred = ref<GitCredentialState>({ has_credential: false });
const credLoading = ref(true);
const credSaving = ref(false);
const credError = ref("");
const username = ref("");
const token = ref("");
const probing = ref(false);
const probe = ref<ConnectionResult | null>(null);
const probeError = ref("");

/** load reads the public key (404 means none yet) and the credential state. */
async function load(): Promise<void> {
  keyLoading.value = true;
  credLoading.value = true;
  keyError.value = "";
  credError.value = "";
  try {
    key.value = await getDeployKey(props.applicationId);
  } catch (error: unknown) {
    key.value = null;
    if (!isNotFound(error)) {
      keyError.value = describeApplicationError(error);
    }
  } finally {
    keyLoading.value = false;
  }
  try {
    cred.value = await getGitCredential(props.applicationId);
    username.value = cred.value.username ?? "";
  } catch (error: unknown) {
    credError.value = describeApplicationError(error);
  } finally {
    credLoading.value = false;
  }
}

/** isNotFound reports a 404-shaped API error (no key stored yet). */
function isNotFound(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "status" in error &&
    (error as { status: unknown }).status === 404
  );
}

/** generateKey creates the local deploy key and shows the public half. */
async function generateKey(): Promise<void> {
  keySaving.value = true;
  keyError.value = "";
  try {
    key.value = await createDeployKey(props.applicationId);
  } catch (error: unknown) {
    keyError.value = describeApplicationError(error);
  } finally {
    keySaving.value = false;
  }
}

/** saveCredential stores (or rotates) the HTTPS token. */
async function saveCredential(): Promise<void> {
  credSaving.value = true;
  credError.value = "";
  try {
    cred.value = await setGitCredential(props.applicationId, username.value.trim(), token.value);
    token.value = "";
  } catch (error: unknown) {
    credError.value = describeApplicationError(error);
  } finally {
    credSaving.value = false;
  }
}

/** runProbe tests the remote with the stored credential and shows the verdict. */
async function runProbe(): Promise<void> {
  probing.value = true;
  probe.value = null;
  probeError.value = "";
  try {
    probe.value = await testConnection(props.applicationId);
  } catch (error: unknown) {
    probeError.value = describeApplicationError(error);
  } finally {
    probing.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>

<template>
  <NCard :title="t('applications.privateGit.title')">
    <NSpace vertical :size="16">
      <div>
        <div class="section-title">{{ t("applications.privateGit.authSsh") }}</div>
        <NAlert v-if="keyError" type="error" :show-icon="true">{{ keyError }}</NAlert>
        <template v-else-if="keyLoading">
          <span class="field-hint">{{ t("applications.privateGit.loading") }}</span>
        </template>
        <template v-else-if="key">
          <pre class="mono key">{{ key.public_key }}</pre>
          <span class="field-hint">{{ t("applications.privateGit.keyHint") }}</span>
          <div style="margin-top: 8px">
            <NButton size="small" @click="void copyText(key.public_key, t('applications.privateGit.publicKey'))">
              {{ t("applications.privateGit.copyKey") }}
            </NButton>
          </div>
        </template>
        <template v-else>
          <div style="margin-top: 8px">
            <NButton size="small" type="primary" :loading="keySaving" @click="void generateKey()">
              {{ t("applications.privateGit.generateKey") }}
            </NButton>
          </div>
        </template>
      </div>

      <div>
        <div class="section-title">{{ t("applications.privateGit.authHttps") }}</div>
        <NAlert v-if="credError" type="error" :show-icon="true">{{ credError }}</NAlert>
        <template v-else-if="credLoading">
          <span class="field-hint">{{ t("applications.privateGit.loading") }}</span>
        </template>
        <template v-else>
          <span class="field-hint">{{
            cred.has_credential
              ? cred.username
                ? t("applications.privateGit.credSetUser", { username: cred.username })
                : t("applications.privateGit.credSet")
              : t("applications.privateGit.credUnset")
          }}</span>
          <div class="form-row">
            <NInput v-model:value="username" class="mono" :placeholder="t('applications.privateGit.username')" autocomplete="off" />
            <NInput
              v-model:value="token"
              class="mono"
              type="password"
              show-password-on="click"
              :placeholder="t('applications.privateGit.token')"
              autocomplete="off"
            />
          </div>
          <div style="margin-top: 8px">
            <NButton size="small" type="primary" :loading="credSaving" :disabled="token.trim() === ''" @click="void saveCredential()">
              {{ t("applications.privateGit.saveCredential") }}
            </NButton>
          </div>
        </template>
      </div>

      <div>
        <NButton size="small" :loading="probing" @click="void runProbe()">
          {{ t("applications.privateGit.test") }}
        </NButton>
        <NAlert v-if="probeError" type="error" :show-icon="true" style="margin-top: 8px">
          {{ probeError }}
        </NAlert>
        <NAlert
          v-else-if="probe"
          :type="probe.ok ? 'success' : 'error'"
          :show-icon="true"
          style="margin-top: 8px"
        >
          {{ probe.ok ? t("applications.privateGit.connected") : probe.message }}
        </NAlert>
      </div>
    </NSpace>
  </NCard>
</template>

<style scoped>
.section-title {
  font-weight: 600;
  margin-bottom: 8px;
}

.field-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}

.key {
  white-space: pre-wrap;
  word-break: break-all;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 8px 12px;
  font-size: var(--text-xs);
}

.form-row {
  display: flex;
  gap: 8px;
  margin-top: 8px;
  flex-wrap: wrap;
}
</style>
