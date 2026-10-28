<script setup lang="ts">
import { NButton, NCard, NTag, NText, useMessage } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import type { DeliveryFailure } from "@/features/notifications/api/notifications";
import {
  deliveryEventLabel,
  describeChannelError,
  isFeatureDisabled,
  kindLabel,
} from "@/features/notifications/api/notifications";
import { useNotificationsStore } from "@/features/notifications/stores/notifications";
import { relativeTime } from "@/shared/utils/format";

const { t } = useI18n();
const message = useMessage();
const store = useNotificationsStore();

const failures = ref<DeliveryFailure[]>([]);
const loadErrorRaw = ref<unknown>(null);
const loadError = computed<string | null>(() =>
  loadErrorRaw.value === null ? null : describeChannelError(loadErrorRaw.value),
);
const retryingId = ref<string | null>(null);

async function load(): Promise<void> {
  loadErrorRaw.value = null;
  try {
    failures.value = await store.failures();
  } catch (error) {
    failures.value = [];
    loadErrorRaw.value = isFeatureDisabled(error) ? null : error;
  }
}

async function retry(failure: DeliveryFailure): Promise<void> {
  retryingId.value = failure.id;
  try {
    await store.retryDelivery(failure.id);
    message.success(t("notifications.attention.retryQueued"));
    await load();
  } catch (error) {
    message.error(describeChannelError(error));
  } finally {
    retryingId.value = null;
  }
}

onMounted(() => {
  void load();
});
</script>

<template>
  <NCard v-if="failures.length > 0" size="small" data-test-delivery-failures>
    <div class="failures">
      <div class="failures-head">
        <NText strong>{{ t("notifications.attention.title") }}</NText>
        <NText depth="3" class="meta">{{ t("notifications.attention.hint") }}</NText>
      </div>
      <ul class="failure-list">
        <li
          v-for="failure in failures"
          :key="failure.id"
          class="failure-row"
          data-test-delivery-failure-row
        >
          <div class="failure-main">
            <NTag size="small" type="error" :bordered="false">
              {{ t("notifications.attention.failed") }}
            </NTag>
            <NText class="name">{{ failure.channel_name }}</NText>
            <NText depth="3" class="meta">{{ kindLabel(failure.channel_kind) }}</NText>
            <NText depth="3" class="meta">
              {{ deliveryEventLabel(failure.event_type) }}
            </NText>
            <NText depth="3" class="meta">
              {{ relativeTime(failure.failed_at) }}
            </NText>
          </div>
          <NText v-if="failure.error_summary" depth="2" class="small error">
            {{ failure.error_summary }}
          </NText>
          <NButton
            v-if="failure.retryable"
            size="tiny"
            secondary
            :loading="retryingId === failure.id"
            @click="void retry(failure)"
          >
            {{ t("notifications.attention.retry") }}
          </NButton>
        </li>
      </ul>
    </div>
  </NCard>
  <NCard v-else-if="loadError" size="small">
    <NText type="error" class="small">{{ loadError }}</NText>
  </NCard>
</template>

<style scoped>
.failures {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.failures-head {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.failure-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.failure-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding: var(--space-2) 0;
  min-width: 0;
}

.failure-row + .failure-row {
  border-top: 1px solid var(--border);
}

.failure-main {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  min-width: 0;
}

.name {
  font-size: var(--text-sm);
}

.meta {
  font-size: var(--text-xs);
}

.small {
  font-size: var(--text-xs);
}

.error {
  font-family: var(--font-mono);
  word-break: break-word;
  flex-basis: 100%;
}
</style>
