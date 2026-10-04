<script setup lang="ts">
import { NAlert, NButton, NCard, NInput, NSpace, NText } from "naive-ui";
import { toRef } from "vue";

import type { Application } from "@/features/applications/api/applications";
import { useApplicationDomain } from "@/features/applications/composables/useApplicationDomain";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const domain = useApplicationDomain(toRef(props, "application"));
</script>

<template>
  <NCard title="Application domain">
    <NSpace vertical :size="12">
      <NText depth="3">
        The control plane routes this hostname to the application container.
        The certificate configuration records it at save time.
      </NText>
      <NAlert
        v-if="domain.domainError.value"
        type="error"
        :show-icon="true"
      >
        {{ domain.domainError.value }}
      </NAlert>
      <NSpace align="center" :size="8" :wrap="false">
        <NInput
          v-model:value="domain.baseDomain.value"
          class="mono"
          style="max-width: 360px"
          placeholder="app.example.com"
          aria-label="Application base domain"
          @keyup.enter="domain.handleSaveDomain"
        />
        <NButton
          type="primary"
          :loading="domain.savingDomain.value"
          @click="domain.handleSaveDomain"
        >
          Save domain
        </NButton>
      </NSpace>
      <NText depth="3" class="small">
        Leave empty to remove the domain. HTTP and HTTPS routing only exist
        while the application has a valid domain.
      </NText>
    </NSpace>
  </NCard>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}
</style>
