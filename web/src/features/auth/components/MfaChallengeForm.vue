<script setup lang="ts">
import { NAlert, NButton, NForm, NFormItem, NInput, NText } from "naive-ui";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useMfaChallenge } from "@/features/auth/composables/useMfaChallenge";

const emit = defineEmits<{ success: []; cancel: []; expired: [] }>();

const { t } = useI18n();
const {
  formRef,
  codeRef,
  form,
  mode,
  placeholder,
  rules,
  submitting,
  errorMessage,
  submit,
  focusCode,
  toggleMode,
} = useMfaChallenge();

onMounted(focusCode);

async function onSubmit(): Promise<void> {
  const outcome = await submit();
  if (outcome === "success") {
    emit("success");
  } else if (outcome === "expired") {
    emit("expired");
  }
}
</script>

<template>
  <NForm ref="formRef" :model="form" :rules="rules" @submit.prevent="onSubmit">
    <NAlert v-if="errorMessage" type="error" :show-icon="true">
      {{ errorMessage }}
    </NAlert>

    <NFormItem
      :label="t('auth.mfa.codeLabel')"
      path="code"
      :label-props="{ for: 'auth-mfa-code' }"
    >
      <NInput
        ref="codeRef"
        v-model:value="form.code"
        autofocus
        :placeholder="placeholder"
        :input-props="{
          id: 'auth-mfa-code',
          autocomplete: 'one-time-code',
          inputmode: mode === 'totp' ? 'numeric' : 'text',
        }"
        @keyup.enter="onSubmit"
      />
    </NFormItem>

    <span class="field-hint">{{ t("auth.mfa.hint") }}</span>

    <NButton text type="primary" class="mfa-toggle" :disabled="submitting" @click="toggleMode">
      {{ mode === "totp" ? t("auth.mfa.useRecovery") : t("auth.mfa.useTotp") }}
    </NButton>

    <div class="mfa-actions">
      <NButton quaternary :disabled="submitting" @click="emit('cancel')">
        {{ t("auth.mfa.back") }}
      </NButton>
      <NButton type="primary" :loading="submitting" @click="onSubmit">
        {{ t("auth.mfa.submit") }}
      </NButton>
    </div>

    <NText v-if="mode === 'totp'" depth="3" class="mfa-recovery">
      {{ t("auth.mfa.recoveryHint") }}
    </NText>
  </NForm>
</template>

<style scoped>
.mfa-toggle {
  margin-top: var(--space-2);
}

.mfa-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-3);
}

.mfa-recovery {
  display: block;
  margin-top: var(--space-2);
  font-size: var(--text-sm);
}
</style>
