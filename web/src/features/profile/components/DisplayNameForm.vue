<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NSpace } from "naive-ui";

import { useDisplayNameForm } from "@/features/profile/composables/useDisplayNameForm";

const { formRef, submitting, errorMessage, form, rules, handleSubmit } =
  useDisplayNameForm();
</script>

<template>
  <NCard title="Display name">
    <NAlert v-if="errorMessage" type="error" :show-icon="true">
      {{ errorMessage }}
    </NAlert>

    <NForm ref="formRef" :model="form" :rules="rules" @submit.prevent="handleSubmit">
      <!-- No visible field label: the card title is the single heading. The
        input keeps its accessible name via aria-label instead of a label
        element, so no reserved label row renders above it. -->
      <NFormItem path="displayName" :show-label="false">
        <NSpace vertical :size="8" class="field-stack">
          <NInput
            v-model:value="form.displayName"
            placeholder="Ada Lovelace"
            maxlength="64"
            :input-props="{
              id: 'profile-display-name',
              autocomplete: 'nickname',
              'aria-label': 'Display name',
            }"
            @keyup.enter="handleSubmit"
          />
          <span class="field-hint">
            Shown in the sidebar instead of your email. Clear it to go back to
            your email. 1-64 characters.
          </span>
        </NSpace>
      </NFormItem>

      <NButton type="primary" :loading="submitting" @click="handleSubmit">
        Save display name
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
/* Same vertical field stack as the password fields (RegisterPage,
 * ChangePasswordForm): the input takes the full form-column width and the
 * hint always renders below it, at any container width. */
.field-stack {
  width: 100%;
}
</style>
