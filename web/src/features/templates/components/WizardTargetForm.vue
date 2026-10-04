<script setup lang="ts">
import { NAlert, NForm, NFormItem, NInput, NSelect } from "naive-ui";
import { computed } from "vue";
import { RouterLink } from "vue-router";

import { useServersStore } from "@/features/servers";

interface Props {
  name: string;
  serverId: string;
  nameError: string;
  nodeError: string;
  createError: string | null;
}

defineProps<Props>();

const emit = defineEmits<{
  "update:name": [value: string];
  "update:serverId": [value: string];
}>();

const serversStore = useServersStore();

const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
  serversStore.servers.map((server) => ({
    label: `${server.name} · ${server.ip}`,
    value: server.id,
  })),
);

const noServers = computed<boolean>(
  () => !serversStore.loading && serversStore.servers.length === 0,
);
</script>

<template>
  <NAlert
    v-if="noServers"
    type="warning"
    :show-icon="true"
  >
    No node is registered yet.
    <RouterLink to="/servers">Add a server</RouterLink>
    before creating a service.
  </NAlert>
  <NForm label-placement="top" class="wizard__metaform">
    <NFormItem
      label="Service name"
      required
      :feedback="nameError"
      :validation-status="nameError ? 'error' : undefined"
      class="field-service-name"
    >
      <NInput
        :value="name"
        aria-label="Service name"
        @update:value="(value) => emit('update:name', value)"
      />
    </NFormItem>
    <NFormItem
      label="Node"
      required
      :feedback="nodeError"
      :validation-status="nodeError ? 'error' : undefined"
      class="field-service-node"
    >
      <NSelect
        :value="serverId"
        :options="serverOptions"
        :loading="serversStore.loading"
        placeholder="Select a node"
        aria-label="Node"
        @update:value="(value) => emit('update:serverId', value)"
      />
    </NFormItem>
  </NForm>
  <NAlert v-if="createError" type="error" :show-icon="true">
    {{ createError }}
  </NAlert>
  <p class="wizard__desc">
    Creating stores the rendered document and its environment
    (<span class="mono">POST /api/v1/services</span>); a service runs
    on exactly one node. Deploy sends the project to that node's agent
    and shows what the agent reports back.
  </p>
</template>

<style scoped>
.wizard__metaform {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 var(--space-4);
}

.wizard__desc {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 720px) {
  .wizard__metaform {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
