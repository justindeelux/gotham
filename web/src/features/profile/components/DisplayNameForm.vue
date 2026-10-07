<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NSpace } from "naive-ui";
import { useI18n } from "vue-i18n";

import { useDisplayNameForm } from "@/features/profile/composables/useDisplayNameForm";

const { t } = useI18n();
const { formRef, submitting, errorMessage, form, rules, handleSubmit } =
  useDisplayNameForm();
</script>

<template>
  <NCard :title="t('profile.displayName.title')">
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
            :placeholder="t('profile.displayName.placeholder')"
            maxlength="64"
            :input-props="{
              id: 'profile-display-name',
              autocomplete: 'nickname',
              'aria-label': t('profile.displayName.ariaLabel'),
            }"
            @keyup.enter="handleSubmit"
          />
          <span class="field-hint">
            {{ t("profile.displayName.hint") }}
          </span>
        </NSpace>
      </NFormItem>

      <NButton type="primary" :loading="submitting" @click="handleSubmit">
        {{ t("profile.displayName.submit") }}
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
