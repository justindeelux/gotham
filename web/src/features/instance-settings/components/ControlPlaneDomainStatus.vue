<script setup lang="ts">
import { NAlert, NButton, NSpace, NTag, NText } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { listRouters } from "@/features/domains/api/proxy";
import type { PanelCertificateState, RouterSyncStatus } from "@/features/domains/api/proxy";
import { controlPlaneUrlSchema } from "@/features/instance-settings/schemas/instance";
import { formatDate } from "@/shared/utils/format";

const props = defineProps<{ url: string; savedUrl: string }>();
const { t, locale } = useI18n();
const reload = ref(0);
const loading = ref(false);
const routerStatus = ref<RouterSyncStatus>("unknown");
const certificate = ref<PanelCertificateState>();
const warning = ref<"missingLocalhost" | "readFailed" | "unsaved" | null>(null);
function parseUrl(value: string): URL | null {
  const result = controlPlaneUrlSchema.safeParse(value);
  return result.success && result.data ? new URL(result.data) : null;
}

const parsedUrl = computed(() => parseUrl(props.url));

watch(
  [() => props.url, () => props.savedUrl, reload],
  (_values, _previous, onCleanup) => {
    let active = true;
    routerStatus.value = "unknown";
    certificate.value = undefined;
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
        } else {
          const router = result.routers.find((entry) =>
            entry.kind === "control_plane" && entry.server_id === node.server_id &&
            new URL(`${url.protocol}//${entry.host}`).origin === url.origin,
          );
          certificate.value = router?.certificate;
          routerStatus.value = router?.entrypoints.includes(
            url.protocol === "https:" ? "websecure" : "web",
          ) ? node.sync_status : "pending";
          if (!router && url.origin !== parseUrl(props.savedUrl)?.origin) {
            warning.value = "unsaved";
          }
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
        <NTag :type="parsedUrl.protocol === 'http:' ? 'default'
          : certificate?.status === 'issued' ? 'success'
          : certificate?.status === 'failed' ? 'error'
          : certificate?.status === 'pending' ? 'warning' : 'default'">
          {{ t(parsedUrl.protocol === "http:"
            ? "instance-settings.domainStatus.certNotApplicable"
            : certificate
              ? `instance-settings.domainStatus.certState.${certificate.status}`
              : "instance-settings.domainStatus.certUnknown") }}
        </NTag>
        <NText v-if="parsedUrl.protocol === 'https:' && certificate?.status === 'issued'
          && certificate.not_after" depth="3">
          {{ t("instance-settings.domainStatus.certExpiry", {
            date: formatDate(certificate.not_after, locale === "vi" ? "vi" : "en"),
          }) }}
        </NText>
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
