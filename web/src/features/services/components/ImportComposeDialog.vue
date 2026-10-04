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
  NSpin,
  NText,
} from "naive-ui";

import { useServicesPageContext } from "@/features/services/composables/useServicesList";
import { useServersStore } from "@/features/servers";

/** ImportComposeDialog renders the compose import modal and its form. */
const serversStore = useServersStore();
const {
  importOpen,
  importName,
  importServerId,
  importYaml,
  importing,
  importError,
  envReference,
  serverOptions,
  importNameError,
  importNodeError,
  handleImport,
} = useServicesPageContext();
</script>

<template>
  <NModal
    v-model:show="importOpen"
    preset="card"
    title="Import compose"
    style="width: 640px; max-width: 96vw"
  >
    <NSpin :show="importing">
      <NAlert v-if="importError" type="error" :show-icon="true" class="mb-3">
        {{ importError }}
      </NAlert>
      <p class="small muted mb-3">
        Paste an existing compose document. The control plane stores it
        verbatim in <span class="mono">services.compose_yaml</span> and
        validates it before saving.
      </p>
      <NForm label-placement="top">
        <div class="import-grid">
          <NFormItem
            label="Service name"
            required
            :feedback="importNameError"
            :validation-status="importNameError ? 'error' : undefined"
            class="field-import-name"
          >
            <NInput
              v-model:value="importName"
              placeholder="blog-staging"
              aria-label="Service name"
            />
          </NFormItem>
          <NFormItem
            label="Node"
            required
            :feedback="importNodeError"
            :validation-status="importNodeError ? 'error' : undefined"
            class="field-import-node"
          >
            <NSelect
              v-model:value="importServerId"
              :options="serverOptions"
              placeholder="Select a node"
              aria-label="Node"
            />
          </NFormItem>
        </div>
        <NFormItem label="compose.yaml" class="field-import-yaml">
          <NInput
            v-model:value="importYaml"
            type="textarea"
            class="mono"
            spellcheck="false"
            :autosize="{ minRows: 10, maxRows: 24 }"
            placeholder="services:&#10;  web:&#10;    image: nginx:1.27-alpine"
            aria-label="compose.yaml"
          />
        </NFormItem>
      </NForm>
      <NText depth="3" class="small">
        <span class="mono">{{ envReference }}</span> references are substituted from
        the environment before the agent validates the document. A compose
        service is routed by the
        <span class="mono">gotham.domain</span> label. Files larger than
        1&nbsp;MiB are rejected by the API.
      </NText>
    </NSpin>
    <template #footer>
      <NSpace :size="8" justify="end">
        <NButton @click="importOpen = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="importing"
          :disabled="serversStore.servers.length === 0"
          @click="handleImport"
        >
          Import service
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.import-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--form-item-gap) var(--space-4);
}

.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

.mb-3 {
  margin-bottom: var(--space-3);
}

@media (max-width: 860px) {
  .import-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
