<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NSpace } from "naive-ui";

import PasswordStrengthMeter from "@/features/auth/components/PasswordStrengthMeter.vue";
import { useAuthStore } from "@/features/auth";
import { useChangePasswordForm } from "@/features/profile/composables/useChangePasswordForm";

const authStore = useAuthStore();
const {
  formRef,
  submitting,
  errorMessage,
  form,
  rules,
  strength,
  hasPassword,
  handleSubmit,
} = useChangePasswordForm();
</script>

<template>
  <NCard title="Change password">
    <NAlert v-if="errorMessage" type="error" :show-icon="true">
      {{ errorMessage }}
    </NAlert>

    <NForm ref="formRef" :model="form" :rules="rules" @submit.prevent="handleSubmit">
      <!-- Hidden username for password managers (Chrome warns without one):
        no name attribute so it is never submitted, readonly + tabindex -1
        so it is never edited or focused, aria-hidden + clipped so it is
        never announced or seen. Value follows the signed-in email. -->
      <input
        class="username-fix"
        type="text"
        autocomplete="username"
        :value="authStore.user?.email ?? ''"
        readonly
        tabindex="-1"
        aria-hidden="true"
      />
      <NFormItem
        v-if="hasPassword"
        label="Current password"
        path="currentPassword"
        :label-props="{ for: 'profile-current-password' }"
      >
        <NInput
          v-model:value="form.currentPassword"
          type="password"
          show-password-on="click"
          placeholder="Your current password"
          :input-props="{
            id: 'profile-current-password',
            autocomplete: 'current-password',
          }"
          @keyup.enter="handleSubmit"
        />
      </NFormItem>

      <div class="form-row">
        <NFormItem
          label="New password"
          path="newPassword"
          :label-props="{ for: 'profile-new-password' }"
        >
          <NSpace vertical :size="8" class="password-field">
            <NInput
              v-model:value="form.newPassword"
              type="password"
              show-password-on="click"
              placeholder="At least 10 characters"
              :input-props="{ id: 'profile-new-password', autocomplete: 'new-password' }"
              @keyup.enter="handleSubmit"
            />
            <PasswordStrengthMeter :score="strength" />
            <span class="field-hint">
              At least 10 characters with 2 character classes: lowercase,
              uppercase, digits, symbols.
            </span>
          </NSpace>
        </NFormItem>

        <NFormItem
          label="Confirm new password"
          path="confirmPassword"
          :label-props="{ for: 'profile-confirm-password' }"
        >
          <NInput
            v-model:value="form.confirmPassword"
            type="password"
            show-password-on="click"
            placeholder="Repeat the new password"
            :input-props="{
              id: 'profile-confirm-password',
              autocomplete: 'new-password',
            }"
            @keyup.enter="handleSubmit"
          />
        </NFormItem>
      </div>

      <NButton type="primary" :loading="submitting" @click="handleSubmit">
        Change password
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
/* Visually hidden without affecting layout: absolute so it takes no space. */
.username-fix {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
  padding: 0;
}
</style>
