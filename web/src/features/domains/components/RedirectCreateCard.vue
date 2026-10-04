<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSpace,
  NSwitch,
  NText,
} from "naive-ui";

import {
  redirectCodeOptions,
  useRedirects,
} from "@/features/domains/composables/useRedirects";

const {
  redirectForm,
  redirectSaving,
  redirectError,
  applicationOptions,
  handleCreateRedirect,
} = useRedirects();
</script>

<template>
  <NCard style="margin-top: 16px" title="Add redirect" class="form-container">
    <template #header-extra>
      <NText depth="3" class="small">
        applies after the next config sync
      </NText>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="redirectError" type="error" :show-icon="true">
        {{ redirectError }}
      </NAlert>
      <NForm label-placement="top" :show-feedback="false" class="redirect-form">
        <div class="redirect-form__fields">
          <NFormItem label="Application" class="field-application">
            <NSelect
              v-model:value="redirectForm.application_id"
              :options="applicationOptions"
              placeholder="Select an application"
              aria-label="Redirect application"
            />
          </NFormItem>
          <NFormItem label="Source domain" class="field-source">
            <NInput
              v-model:value="redirectForm.source_domain"
              placeholder="shop.example.com"
              aria-label="Redirect source domain"
            />
          </NFormItem>
          <NFormItem label="Target domain" class="field-target">
            <NInput
              v-model:value="redirectForm.target_domain"
              placeholder="storefront.example.com"
              aria-label="Redirect target domain"
            />
          </NFormItem>
          <NFormItem label="Redirect code" class="field-code">
            <NSelect
              v-model:value="redirectForm.code"
              :options="redirectCodeOptions"
              aria-label="Redirect code"
            />
          </NFormItem>
        </div>
        <div class="redirect-form__bottom">
          <NFormItem label="Preserve path" class="field-preserve">
            <NSwitch
              v-model:value="redirectForm.preserve_path"
              aria-label="Redirect preserve path"
            />
          </NFormItem>
          <NFormItem label="Enabled" class="field-enabled">
            <NSwitch
              v-model:value="redirectForm.enabled"
              aria-label="Redirect enabled now"
            />
          </NFormItem>
          <NButton
            type="primary"
            :loading="redirectSaving"
            @click="handleCreateRedirect"
          >
            Add redirect
          </NButton>
        </div>
      </NForm>
      <NText depth="3" class="small">
        Source and target must differ. The code applies to GET; HEAD
        and every other method answer 308/307, keeping the method.
      </NText>
    </NSpace>
  </NCard>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

.redirect-form__fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(172px, 1fr));
  gap: var(--form-item-gap);
  align-items: start;
}

.redirect-form__fields .n-form-item {
  margin: 0;
  min-width: 0;
}

/* Preserve path + Enabled share one row with the submit button, so the
 * inline form ends in a single action row instead of two lone switches. */
.redirect-form__bottom {
  display: flex;
  align-items: flex-end;
  gap: var(--space-4);
  flex-wrap: wrap;
  margin-top: var(--form-item-gap);
}

.redirect-form__bottom .n-form-item {
  margin: 0;
  min-width: 0;
}

@container (max-width: 480px) {
  .redirect-form__bottom {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
