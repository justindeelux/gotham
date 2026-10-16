<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSpace,
  NSpin,
  NText,
} from "naive-ui";
import type { FormInst } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeAuthError, useAuthStore } from "@/features/auth";
import {
  beginTotpEnrollment,
  enableTotp,
  type TotpEnrollment,
} from "@/features/profile/api/totp";
import { totpCodeRules } from "@/features/profile/schemas/totp";
import QrCode from "@/shared/components/QrCode.vue";
import { useCopyText } from "@/shared/composables/useCopyText";

/**
 * TotpSetupDialog runs the two-factor enrollment (JUS-196): it shows the
 * authenticator QR plus the manual key, confirms the first code (with the
 * account password when one is set), and finally reveals the recovery codes
 * once. Nothing is enabled until the confirmation succeeds.
 */
const props = defineProps<{
  show: boolean;
}>();

const emit = defineEmits<{
  "update:show": [value: boolean];
  /** done reports that two-factor is now enabled, so the card refreshes. */
  done: [];
}>();

const { t } = useI18n();
const authStore = useAuthStore();
const { copyText } = useCopyText();

const hasPassword = computed<boolean>(() => authStore.user?.has_password ?? true);

const step = ref<"scan" | "codes">("scan");
const loading = ref(false);
const submitting = ref(false);
const enrolling = ref<TotpEnrollment | null>(null);
const recoveryCodes = ref<string[]>([]);
const rawError = ref<unknown>(null);
const errorMessage = computed<string>(() =>
  rawError.value === null ? "" : describeAuthError(rawError.value),
);

const formRef = ref<FormInst | null>(null);
const form = reactive({ password: "", code: "" });
const rules = totpCodeRules(hasPassword.value);

/** startEnrollment mints a fresh pending secret every time the dialog opens. */
async function startEnrollment(): Promise<void> {
  step.value = "scan";
  enrolling.value = null;
  recoveryCodes.value = [];
  rawError.value = null;
  form.password = "";
  form.code = "";
  loading.value = true;
  try {
    enrolling.value = await beginTotpEnrollment();
  } catch (error) {
    rawError.value = error;
  } finally {
    loading.value = false;
  }
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      void startEnrollment();
    }
  },
);

/** confirmEnable verifies the first code and turns two-factor on. */
async function confirmEnable(): Promise<void> {
  if (submitting.value) {
    return;
  }
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  submitting.value = true;
  rawError.value = null;
  try {
    recoveryCodes.value = await enableTotp({
      ...(hasPassword.value ? { password: form.password } : {}),
      code: form.code,
    });
    form.password = "";
    form.code = "";
    step.value = "codes";
  } catch (error) {
    rawError.value = error;
  } finally {
    submitting.value = false;
  }
}

/** finish closes the dialog after the recovery codes were acknowledged. */
function finish(): void {
  emit("update:show", false);
  emit("done");
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="t('profile.totp.setupTitle')"
    class="app-modal"
    style="width: 520px; max-width: 94vw"
    @update:show="emit('update:show', $event)"
  >
    <NSpace vertical :size="16">
      <NAlert v-if="errorMessage" type="error" :show-icon="true">
        {{ errorMessage }}
      </NAlert>

      <NSpin v-if="loading" size="large" class="totp-spin" />

      <!-- Step 1: scan the QR (or type the key) and confirm a code. -->
      <template v-else-if="step === 'scan' && enrolling">
        <NText depth="3">{{ t("profile.totp.scanHint") }}</NText>

        <div class="totp-qr">
          <QrCode
            :value="enrolling.otpauth_url"
            :label="t('profile.totp.qrLabel')"
          />
        </div>

        <div class="totp-secret">
          <NText depth="3">{{ t("profile.totp.manualHint") }}</NText>
          <code class="totp-secret-value">{{ enrolling.secret }}</code>
          <NButton
            size="small"
            quaternary
            @click="copyText(enrolling.secret, t('profile.totp.secretLabel'))"
          >
            {{ t("profile.totp.copySecret") }}
          </NButton>
        </div>

        <NForm ref="formRef" :model="form" :rules="rules" @submit.prevent="confirmEnable">
          <NFormItem
            v-if="hasPassword"
            :label="t('profile.totp.passwordLabel')"
            path="password"
            :label-props="{ for: 'totp-enroll-password' }"
          >
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              :placeholder="t('profile.totp.passwordPlaceholder')"
              :input-props="{ id: 'totp-enroll-password', autocomplete: 'current-password' }"
              @keyup.enter="confirmEnable"
            />
          </NFormItem>

          <NFormItem
            :label="t('profile.totp.codeLabel')"
            path="code"
            :label-props="{ for: 'totp-enroll-code' }"
          >
            <NInput
              v-model:value="form.code"
              :placeholder="t('profile.totp.codePlaceholder')"
              :input-props="{ id: 'totp-enroll-code', autocomplete: 'one-time-code' }"
              @keyup.enter="confirmEnable"
            />
          </NFormItem>

          <NButton type="primary" :loading="submitting" @click="confirmEnable">
            {{ t("profile.totp.confirmEnable") }}
          </NButton>
        </NForm>
      </template>

      <!-- Step 2: the recovery codes, shown once. -->
      <template v-else-if="step === 'codes'">
        <NText depth="3">{{ t("profile.totp.codesHint") }}</NText>
        <ul class="totp-codes">
          <li v-for="code in recoveryCodes" :key="code">
            <code>{{ code }}</code>
          </li>
        </ul>
        <NButton
          size="small"
          quaternary
          @click="copyText(recoveryCodes.join('\n'), t('profile.totp.codesLabel'))"
        >
          {{ t("profile.totp.copyCodes") }}
        </NButton>
        <NButton type="primary" @click="finish">
          {{ t("profile.totp.codesSaved") }}
        </NButton>
      </template>
    </NSpace>
  </NModal>
</template>

<style scoped>
.totp-spin {
  display: flex;
  justify-content: center;
  padding: var(--space-5) 0;
}

.totp-qr {
  display: flex;
  justify-content: center;
}

.totp-secret {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}

.totp-secret-value {
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
  font-family: var(--font-mono);
  letter-spacing: 0.08em;
  word-break: break-all;
}

.totp-codes {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.totp-codes code {
  font-family: var(--font-mono);
  letter-spacing: 0.08em;
}
</style>
