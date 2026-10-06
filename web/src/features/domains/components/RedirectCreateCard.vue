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
import { computed } from "vue";

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

/** codeOptions refreshes the redirect-code labels on a language switch. */
const codeOptions = computed(() => redirectCodeOptions());
</script>

<template>
  <NCard style="margin-top: 16px" :title="$t('domains.redirects.addTitle')" class="form-container">
    <template #header-extra>
      <NText depth="3" class="small">
        {{ $t("domains.redirects.appliesAfter") }}
      </NText>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="redirectError" type="error" :show-icon="true">
        {{ redirectError }}
      </NAlert>
      <NForm label-placement="top" :show-feedback="false" class="redirect-form">
        <div class="redirect-form__fields">
          <NFormItem :label="$t('domains.redirects.application')" class="field-application">
            <NSelect
              v-model:value="redirectForm.application_id"
              :options="applicationOptions"
              :placeholder="$t('domains.redirects.selectApplication')"
              :aria-label="$t('domains.redirects.applicationAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('domains.redirects.sourceDomain')" class="field-source">
            <NInput
              v-model:value="redirectForm.source_domain"
              placeholder="shop.example.com"
              :aria-label="$t('domains.redirects.sourceAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('domains.redirects.targetDomain')" class="field-target">
            <NInput
              v-model:value="redirectForm.target_domain"
              placeholder="storefront.example.com"
              :aria-label="$t('domains.redirects.targetAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('domains.redirects.redirectCode')" class="field-code">
            <NSelect
              v-model:value="redirectForm.code"
              :options="codeOptions"
              :aria-label="$t('domains.redirects.codeAria')"
            />
          </NFormItem>
        </div>
        <div class="redirect-form__bottom">
          <NFormItem :label="$t('domains.redirects.preservePath')" class="field-preserve">
            <NSwitch
              v-model:value="redirectForm.preserve_path"
              :aria-label="$t('domains.redirects.preserveAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('domains.redirects.enabled')" class="field-enabled">
            <NSwitch
              v-model:value="redirectForm.enabled"
              :aria-label="$t('domains.redirects.enabledNowAria')"
            />
          </NFormItem>
          <NButton
            type="primary"
            :loading="redirectSaving"
            @click="handleCreateRedirect"
          >
            {{ $t("domains.redirects.addRedirect") }}
          </NButton>
        </div>
      </NForm>
      <NText depth="3" class="small">
        {{ $t("domains.redirects.ruleHint") }}
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
