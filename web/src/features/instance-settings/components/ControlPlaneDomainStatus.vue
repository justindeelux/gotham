<script setup lang="ts">
import { NAlert, NButton, NSpace, NTag, NText } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { listRouters } from "@/features/domains/api/proxy";
import type { RouterSyncStatus } from "@/features/domains/api/proxy";
import { controlPlaneUrlSchema } from "@/features/instance-settings/schemas/instance";

const props = defineProps<{ url: string; savedUrl: string }>();
const { t } = useI18n();
const reload = ref(0);
const loading = ref(false);
const routerStatus = ref<RouterSyncStatus>("unknown");
const warning = ref<"missingLocalhost" | "readFailed" | "unsaved" | null>(null);
const parsedUrl = computed(() => {
  const result = controlPlaneUrlSchema.safeParse(props.url);
  return result.success && result.data ? new URL(result.data) : null;
});

watch(
  [() => props.url, () => props.savedUrl, reload],
  (_values, _previous, onCleanup) => {
    let active = true;
    routerStatus.value = "unknown";
    warning.value = null;
    loading.value = parsedUrl.value !== null;
    const url = parsedUrl.value;
    const timer = setTimeout(async () => {
      if (!url) return;
      try {
        const result = await listRouters();
        if (!active) return;
        const node = result.nodes.find((entry) => entry.server_name === "localhost");
        if (!node) {
          warning.value = "missingLocalhost";
        } else if (node.error) {
          warning.value = "readFailed";
        } else if (props.url.trim() !== props.savedUrl.trim()) {
          warning.value = "unsaved";
          routerStatus.value = "pending";
        } else {
          const router = result.routers.find((entry) =>
            entry.kind === "control_plane" && entry.server_id === node.server_id &&
            entry.host === url.hostname &&
            entry.entrypoints.includes(url.protocol === "https:" ? "websecure" : "web"),
          );
          routerStatus.value = router ? node.sync_status : "pending";
        }
      } catch {
        if (active) warning.value = "readFailed";
      } finally {
        if (active) loading.value = false;
      }
    }, 300);
    onCleanup(() => {
      active = false;
      clearTimeout(timer);
    });
  },
  { immediate: true },
);
</script>

<template>
  <NSpace
    vertical
    class="domain-status"
    role="status"
    aria-live="polite"
    :aria-label="t('instance-settings.domainStatus.title')"
  >
    <NText v-if="!url.trim()" depth="3">
      {{ t("instance-settings.domainStatus.empty") }}
    </NText>
    <NText v-else-if="!parsedUrl" depth="3">
      {{ t("instance-settings.domainStatus.invalid") }}
    </NText>
    <template v-else>
      <NSpace align="center">
        <NTag>{{ t("instance-settings.domainStatus.dnsUnknown") }}</NTag>
        <NTag>
          {{ t(parsedUrl.protocol === "http:"
            ? "instance-settings.domainStatus.certNotApplicable"
            : "instance-settings.domainStatus.certUnknown") }}
        </NTag>
        <NTag :type="!loading && routerStatus === 'synced' ? 'success' : 'warning'">
          {{ t(`instance-settings.domainStatus.${loading ? 'loading' : routerStatus}`) }}
        </NTag>
        <NButton size="small" :loading="loading" @click="reload++">
          {{ t("instance-settings.domainStatus.refresh") }}
        </NButton>
      </NSpace>
      <NText depth="3">{{ t("instance-settings.domainStatus.observationHint") }}</NText>
      <NAlert v-if="warning" type="warning" :show-icon="true">
        {{ t(`instance-settings.domainStatus.${warning}`) }}
      </NAlert>
    </template>
  </NSpace>
</template>

<style scoped>
.domain-status {
  margin-bottom: var(--space-4);
}
</style>
