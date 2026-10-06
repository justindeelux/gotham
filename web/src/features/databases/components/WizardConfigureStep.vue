<script setup lang="ts">
import {
  NAlert,
  NFormItem,
  NInput,
  NInputNumber,
  NSpace,
  NSwitch,
  NText,
} from "naive-ui";
import { inject } from "vue";

import { wizardFormKey } from "@/features/databases/composables/useCreateDatabaseWizard";
import { isDatabaseNameValid } from "@/features/databases/schemas/databases";
import { resolveValidationMessage } from "@/shared/i18n";

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const form = inject(wizardFormKey)!;
</script>

<template>
  <NFormItem
    :label="t('databases.wizard.configure.name')"
    :feedback="
      form.name === '' || isDatabaseNameValid(form.name)
        ? t('databases.wizard.configure.nameFeedback')
        : resolveValidationMessage('databases.validation.nameRule')
    "
    :validation-status="
      form.name === '' || isDatabaseNameValid(form.name)
        ? undefined
        : 'error'
    "
  >
    <NInput
      v-model:value="form.name"
      class="mono"
      :placeholder="t('databases.wizard.configure.namePlaceholder')"
    />
  </NFormItem>
  <NFormItem :show-feedback="false">
    <NSpace align="center" :size="12">
      <NSwitch v-model:value="form.exposePublic" />
      <NText>{{ t("databases.wizard.configure.expose") }}</NText>
    </NSpace>
  </NFormItem>
  <NAlert v-if="form.exposePublic" type="warning" :show-icon="true">
    {{ t("databases.wizard.configure.exposeWarning") }}
  </NAlert>
  <NFormItem
    v-if="form.exposePublic"
    :label="t('databases.wizard.configure.publicPort')"
    :feedback="t('databases.wizard.configure.publicPortFeedback')"
  >
    <NInputNumber
      v-model:value="form.publicPort"
      :min="1"
      :max="65535"
      :placeholder="t('databases.wizard.configure.publicPortPlaceholder')"
      style="width: 100%"
    />
  </NFormItem>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
