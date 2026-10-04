<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput } from "naive-ui";

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
      >
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
      </NFormItem>

      <NButton
        type="primary"
        :loading="submitting"
        attr-type="submit"
        @click="handleSubmit"
      >
        Save display name
      </NButton>
    </NForm>
  </NCard>
</template>
