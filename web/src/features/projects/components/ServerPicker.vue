<script setup lang="ts">
import { NAlert, NButton, NFormItem, NSelect, NText } from "naive-ui";
import { computed, onMounted, watch } from "vue";
import { RouterLink } from "vue-router";

import { useServersStore } from "@/features/servers";
import {
  buildServerOptions,
  singleUsableServerId,
  unusableServerHint,
} from "@/features/projects/utils/serverOptions";
import { activeLocale, i18n } from "@/shared/i18n";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    label?: string;
    feedback?: string;
    disabled?: boolean;
  }>(),
  { label: undefined, feedback: "", disabled: false },
);

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

const serversStore = useServersStore();

const options = computed(() => buildServerOptions(serversStore.servers));

const blockedHint = computed(() => unusableServerHint(serversStore.servers));

function t(key: string): string {
  void activeLocale.value;
  return String(i18n.global.t(key));
}

const labelText = computed<string>(() => props.label ?? t("projects.server.label"));

const noServers = computed<boolean>(
  () =>
    !serversStore.loading &&
    serversStore.error === null &&
    serversStore.servers.length === 0,
);

function reload(): void {
  void serversStore.fetchServers().catch(() => undefined);
}

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
  reload();
});
</script>

<template>
  <NFormItem
    :label="labelText"
    required
    :feedback="props.feedback"
    :validation-status="props.feedback ? 'error' : undefined"
  >
    <NSelect
      :value="props.modelValue"
      :options="options"
      :loading="serversStore.loading"
      :disabled="props.disabled"
      :placeholder="t('projects.server.placeholder')"
      :aria-label="t('projects.server.aria')"
      @update:value="(value: string) => emit('update:modelValue', value)"
    />
    <template v-if="serversStore.error">
      <NAlert type="error" :show-icon="true">
        {{ serversStore.error }}
      </NAlert>
      <NButton size="small" @click="reload">{{ t("common.actions.retry") }}</NButton>
    </template>
    <template v-else-if="noServers">
      <NText depth="3">
        {{ t("projects.server.emptyPrefix") }}
        <RouterLink to="/servers">{{ t("projects.server.emptyLink") }}</RouterLink>
        {{ t("projects.server.emptySuffix") }}
      </NText>
    </template>
    <template v-else-if="blockedHint">
      <NText depth="3">{{ blockedHint }} {{ t("projects.server.cannotHost") }}</NText>
    </template>
  </NFormItem>
</template>
