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
</script>

<template>
  <NModal
    v-model:show="redirectEditOpen"
    preset="card"
    title="Edit redirect rule"
    style="width: 560px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="redirectEditError" type="error" :show-icon="true">
        {{ redirectEditError }}
      </NAlert>
      <NText depth="3" class="small">
        Application:
        <span class="mono">
          {{ applicationName(redirectEditForm.application_id) }}
        </span>
        — the owning application cannot be moved after creation.
      </NText>
      <NForm label-placement="top" :show-feedback="false">
        <NFormItem label="Source domain">
          <NInput
            v-model:value="redirectEditForm.source_domain"
            aria-label="Edit redirect source domain"
          />
        </NFormItem>
        <NFormItem label="Target domain">
          <NInput
            v-model:value="redirectEditForm.target_domain"
            aria-label="Edit redirect target domain"
          />
        </NFormItem>
        <NFormItem label="Redirect code">
          <NSelect
            v-model:value="redirectEditForm.code"
            :options="redirectCodeOptions"
            aria-label="Edit redirect code"
          />
        </NFormItem>
        <NFormItem label="Preserve path">
          <NSwitch
            v-model:value="redirectEditForm.preserve_path"
            aria-label="Edit redirect preserve path"
          />
        </NFormItem>
        <NFormItem label="Enabled">
          <NSwitch
            v-model:value="redirectEditForm.enabled"
            aria-label="Edit redirect enabled"
          />
        </NFormItem>
      </NForm>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="redirectEditOpen = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="redirectEditSaving"
          @click="handleSaveRedirect"
        >
          Save
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
