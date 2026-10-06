<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NSwitch,
  NText,
} from "naive-ui";
import { computed } from "vue";

import { useDomainLabels } from "@/features/domains/composables/useDomainLabels";
import {
  redirectCodeOptions,
  useRedirects,
} from "@/features/domains/composables/useRedirects";

const {
  redirectEditOpen,
  redirectEditSaving,
  redirectEditError,
  redirectEditForm,
  handleSaveRedirect,
} = useRedirects();
const { applicationName } = useDomainLabels();

/** codeOptions refreshes the redirect-code labels on a language switch. */
const codeOptions = computed(() => redirectCodeOptions());
</script>

<template>
  <NModal
    v-model:show="redirectEditOpen"
    preset="card"
    :title="$t('domains.redirects.editTitle')"
    style="width: 560px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="redirectEditError" type="error" :show-icon="true">
        {{ redirectEditError }}
      </NAlert>
      <NText depth="3" class="small">
        {{ $t("domains.redirects.owningApp") }}
        <span class="mono">
          {{ applicationName(redirectEditForm.application_id) }}
        </span>
        {{ $t("domains.redirects.cannotMove") }}
      </NText>
      <NForm label-placement="top" :show-feedback="false">
        <NFormItem :label="$t('domains.redirects.sourceDomain')">
          <NInput
            v-model:value="redirectEditForm.source_domain"
            :aria-label="$t('domains.redirects.editSourceAria')"
          />
        </NFormItem>
        <NFormItem :label="$t('domains.redirects.targetDomain')">
          <NInput
            v-model:value="redirectEditForm.target_domain"
            :aria-label="$t('domains.redirects.editTargetAria')"
          />
        </NFormItem>
        <NFormItem :label="$t('domains.redirects.redirectCode')">
          <NSelect
            v-model:value="redirectEditForm.code"
            :options="codeOptions"
            :aria-label="$t('domains.redirects.editCodeAria')"
          />
        </NFormItem>
        <NFormItem :label="$t('domains.redirects.preservePath')">
          <NSwitch
            v-model:value="redirectEditForm.preserve_path"
            :aria-label="$t('domains.redirects.editPreserveAria')"
          />
        </NFormItem>
        <NFormItem :label="$t('domains.redirects.enabled')">
          <NSwitch
            v-model:value="redirectEditForm.enabled"
            :aria-label="$t('domains.redirects.editEnabledAria')"
          />
        </NFormItem>
      </NForm>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="redirectEditOpen = false">{{ $t("domains.redirects.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="redirectEditSaving"
          @click="handleSaveRedirect"
        >
          {{ $t("domains.redirects.save") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-xs);
}
</style>
