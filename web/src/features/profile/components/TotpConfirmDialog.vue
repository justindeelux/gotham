<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSpace,
  NText,
  useMessage,
} from "naive-ui";
import type { FormInst } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeAuthError, useAuthStore } from "@/features/auth";
import {
  disableTotp,
  regenerateTotpRecoveryCodes,
} from "@/features/profile/api/totp";
import { totpCodeRules } from "@/features/profile/schemas/totp";
import { useCopyText } from "@/shared/composables/useCopyText";
import { downloadText } from "@/shared/utils/download";

/**
 * TotpConfirmDialog asks for the password and a current code before turning
 * two-factor off or regenerating the recovery codes (JUS-196), so a stolen
 * interactive session cannot do either alone. Regenerating then reveals the
 * fresh codes once.
 */
const props = defineProps<{
  show: boolean;
  mode: "disable" | "regenerate";
}>();

const emit = defineEmits<{
  "update:show": [value: boolean];
  /** done reports the change was committed, so the card refreshes. */
  done: [];
}>();

const { t } = useI18n();
const authStore = useAuthStore();
const message = useMessage();
const { copyText } = useCopyText();

const hasPassword = computed<boolean>(() => authStore.user?.has_password ?? true);

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const rawError = ref<unknown>(null);
const errorMessage = computed<string>(() =>
  rawError.value === null ? "" : describeAuthError(rawError.value),
);
const form = reactive({ password: "", code: "" });
// A computed so the password rule follows a late /auth/me load.
const rules = computed(() => totpCodeRules(hasPassword.value));

const codes = ref<string[]>([]);

/**
 * gateCodes is true while the freshly regenerated recovery codes are on screen
 * (and not yet acknowledged), so the modal is not dismissable then and the codes
 * cannot be lost.
 */
const gateCodes = computed<boolean>(() => codes.value.length > 0);

/** downloadCodes saves the regenerated codes as a text file. */
function downloadCodes(): void {
  downloadText("gotham-recovery-codes.txt", `${codes.value.join("\n")}\n`);
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      form.password = "";
      form.code = "";
      rawError.value = null;
      codes.value = [];
    }
  },
);

const title = computed(() =>
  props.mode === "disable"
    ? t("profile.totp.disableTitle")
    : t("profile.totp.regenerateTitle"),
);
const hint = computed(() =>
  props.mode === "disable"
    ? t("profile.totp.disableHint")
    : t("profile.totp.regenerateHint"),
);
const submitLabel = computed(() =>
  props.mode === "disable"
    ? t("profile.totp.disableSubmit")
    : t("profile.totp.regenerateSubmit"),
);

/** submit runs the chosen action after validating the fields. */
async function submit(): Promise<void> {
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
    const input = {
      ...(hasPassword.value ? { password: form.password } : {}),
      code: form.code,
    };
    if (props.mode === "disable") {
      await disableTotp(input);
      message.success(t("profile.totp.disabledToast"));
      close(true);
      return;
    }
    codes.value = await regenerateTotpRecoveryCodes(input);
    form.password = "";
    form.code = "";
    message.success(t("profile.totp.regeneratedToast"));
  } catch (error) {
    rawError.value = error;
  } finally {
    submitting.value = false;
  }
}

/** close dismisses the dialog, reporting whether the state changed. */
function close(changed: boolean): void {
  emit("update:show", false);
  if (changed) {
    emit("done");
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="title"
    class="app-modal"
    style="width: 480px; max-width: 94vw"
    :mask-closable="!gateCodes"
    :close-on-esc="!gateCodes"
    @update:show="emit('update:show', $event)"
  >
    <NSpace vertical :size="16">
      <NAlert v-if="errorMessage" type="error" :show-icon="true">
        {{ errorMessage }}
      </NAlert>

      <!-- The fresh codes replace the form once regeneration succeeded. -->
      <template v-if="codes.length > 0">
        <NText depth="3">{{ t("profile.totp.codesHint") }}</NText>
        <ul class="totp-codes">
          <li v-for="code in codes" :key="code">
            <code>{{ code }}</code>
          </li>
        </ul>
        <NSpace :size="8">
          <NButton
            size="small"
            quaternary
            @click="copyText(codes.join('\n'), t('profile.totp.codesLabel'))"
          >
            {{ t("profile.totp.copyCodes") }}
          </NButton>
          <NButton size="small" quaternary @click="downloadCodes">
            {{ t("profile.totp.downloadCodes") }}
          </NButton>
          <NButton type="primary" @click="close(true)">
            {{ t("profile.totp.codesSaved") }}
          </NButton>
        </NSpace>
      </template>

      <template v-else>
        <NText depth="3">{{ hint }}</NText>

        <NForm ref="formRef" :model="form" :rules="rules" @submit.prevent="submit">
          <NFormItem
            v-if="hasPassword"
            :label="t('profile.totp.passwordLabel')"
            path="password"
            :label-props="{ for: 'totp-confirm-password' }"
          >
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              :placeholder="t('profile.totp.passwordPlaceholder')"
              :input-props="{ id: 'totp-confirm-password', autocomplete: 'current-password' }"
              @keyup.enter="submit"
            />
          </NFormItem>

          <NFormItem
            :label="t('profile.totp.codeLabel')"
            path="code"
            :label-props="{ for: 'totp-confirm-code' }"
          >
            <NInput
              v-model:value="form.code"
              :placeholder="t('profile.totp.codePlaceholder')"
              :input-props="{ id: 'totp-confirm-code', autocomplete: 'one-time-code' }"
              @keyup.enter="submit"
            />
          </NFormItem>

          <span class="field-hint">{{ t("profile.totp.codeHint") }}</span>

          <NSpace justify="end" :size="8" class="totp-actions">
            <NButton :disabled="submitting" @click="close(false)">
              {{ t("profile.totp.cancel") }}
            </NButton>
            <NButton
              :type="props.mode === 'disable' ? 'error' : 'primary'"
              :loading="submitting"
              @click="submit"
            >
              {{ submitLabel }}
            </NButton>
          </NSpace>
        </NForm>
      </template>
    </NSpace>
  </NModal>
</template>

<style scoped>
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

.totp-actions {
  margin-top: var(--space-3);
}
</style>
