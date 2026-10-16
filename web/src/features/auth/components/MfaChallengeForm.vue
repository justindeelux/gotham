<script setup lang="ts">
import { NAlert, NButton, NForm, NFormItem, NInput, NText } from "naive-ui";
import { useI18n } from "vue-i18n";

import { useMfaChallenge } from "@/features/auth/composables/useMfaChallenge";

/**
 * MfaChallengeForm is the second login step (JUS-196): the account's password
 * (or OAuth identity) was accepted, but no session exists yet. It emits
 * "success" once the code was verified and the session started, and "cancel"
 * to go back to the credentials step.
 */
const emit = defineEmits<{ success: []; cancel: [] }>();

const { t } = useI18n();
const { formRef, form, rules, submitting, errorMessage, submit } = useMfaChallenge();

async function onSubmit(): Promise<void> {
  if (await submit()) {
    emit("success");
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
        v-model:value="form.code"
        :placeholder="t('auth.mfa.codePlaceholder')"
        :input-props="{ id: 'auth-mfa-code', autocomplete: 'one-time-code' }"
        @keyup.enter="onSubmit"
      />
    </NFormItem>

    <span class="field-hint">{{ t("auth.mfa.hint") }}</span>

    <div class="mfa-actions">
      <NButton quaternary :disabled="submitting" @click="emit('cancel')">
        {{ t("auth.mfa.back") }}
      </NButton>
      <NButton type="primary" :loading="submitting" @click="onSubmit">
        {{ t("auth.mfa.submit") }}
      </NButton>
    </div>

    <NText depth="3" class="mfa-recovery">{{ t("auth.mfa.recoveryHint") }}</NText>
  </NForm>
</template>

<style scoped>
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
