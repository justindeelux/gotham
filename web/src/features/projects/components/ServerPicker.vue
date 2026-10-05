<script setup lang="ts">
import { NFormItem, NSelect, NText } from "naive-ui";
import { computed, onMounted, watch } from "vue";
import { RouterLink } from "vue-router";

import { useServersStore } from "@/features/servers";
import {
  buildServerOptions,
  singleUsableServerId,
  unusableServerHint,
} from "@/features/projects/utils/serverOptions";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    label?: string;
    feedback?: string;
  }>(),
  { label: "Node", feedback: "" },
);

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

/**
 * Required server picker shared by every create flow and the resource move
 * card (PE-5, Linear JUS-34). Nodes that are not `ready` are disabled with
 * the reason listed under the field; a single usable node is preselected.
 */
const serversStore = useServersStore();

const options = computed(() => buildServerOptions(serversStore.servers));

const blockedHint = computed(() => unusableServerHint(serversStore.servers));

const noServers = computed<boolean>(
  () => !serversStore.loading && serversStore.servers.length === 0,
);

// Preselect when a single usable node exists and nothing is picked yet.
watch(
  () => serversStore.servers,
  (servers) => {
    if (props.modelValue !== "") {
      return;
    }
    const preselected = singleUsableServerId(servers);
    if (preselected !== "") {
      emit("update:modelValue", preselected);
    }
  },
  { immediate: true },
);

onMounted(() => {
  void serversStore.fetchServers().catch(() => undefined);
});
</script>

<template>
  <NFormItem
    :label="props.label"
    required
    :feedback="props.feedback"
    :validation-status="props.feedback ? 'error' : undefined"
  >
    <NSelect
      :value="props.modelValue"
      :options="options"
      :loading="serversStore.loading"
      placeholder="Select a node"
      aria-label="Node"
      @update:value="(value: string) => emit('update:modelValue', value)"
    />
    <template v-if="noServers">
      <NText depth="3">
        No node is registered yet.
        <RouterLink to="/servers">Add a server</RouterLink>
        before creating a resource.
      </NText>
    </template>
    <template v-else-if="blockedHint">
      <NText depth="3">{{ blockedHint }} — cannot host resources.</NText>
    </template>
    <template v-else>
      <NText depth="3">The resource runs on this node.</NText>
    </template>
  </NFormItem>
</template>
