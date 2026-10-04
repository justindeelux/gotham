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
      <NFormItem
        label="Display name"
        path="displayName"
        :label-props="{ for: 'profile-display-name' }"
        class="display-name-item"
      >
        <NSpace vertical :size="8" class="field-stack">
          <NInput
            v-model:value="form.displayName"
            placeholder="Ada Lovelace"
            maxlength="64"
            :input-props="{ id: 'profile-display-name', autocomplete: 'nickname' }"
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

/* The card title is the visible heading; the field label stays in the DOM
 * for the label/input association but is visually hidden. */
.display-name-item :deep(.n-form-item-label) {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
</style>
