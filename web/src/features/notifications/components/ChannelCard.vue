<script setup lang="ts">
import { NButton, NCard, NPopconfirm, NSpace, NSwitch, NTag, NText, useMessage } from "naive-ui";
import { computed, ref } from "vue";

import type {
  NotificationChannel,
  NotificationDelivery,
  TestResult,
} from "@/features/notifications/api/notifications";
import {
  channelText,
  deliveryEventLabel,
  deliveryStatusLabel,
  describeChannelError,
  eventLabel,
  kindLabel,
} from "@/features/notifications/api/notifications";
import { useChannelDialog } from "@/features/notifications/composables/useChannelDialog";
import { useNotificationsStore } from "@/features/notifications/stores/notifications";
import {
  configSummary,
  scopeLabel,
} from "@/features/notifications/utils/channelHelpers";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  channel: NotificationChannel;
}

const props = defineProps<Props>();

const message = useMessage();
const channelsStore = useNotificationsStore();
const { canMutate, openEdit, resolveResourceName } = useChannelDialog();

const testing = ref(false);
/** Last server answer, rendered verbatim (wire text, never translated). */
const testResult = ref<TestResult | null>(null);
/**
 * Raw failure behind a failed test run. The display text derives from it
 * plus the current locale, so a language switch refreshes a retained
 * failure without resending the test (no outbound delivery on switch).
 */
const testErrorRaw = ref<unknown>(null);
const testFailure = computed<string | null>(() =>
  testErrorRaw.value === null
    ? null
    : describeChannelError(testErrorRaw.value),
);

/**
 * Delivery-history panel state. The panel is lazy: it reads the history only
 * when the operator opens it (and again after a send-test), so the page never
 * fetches history for channels nobody inspects.
 */
const historyOpen = ref(false);
const historyLoading = ref(false);
const deliveries = ref<NotificationDelivery[]>([]);
const historyErrorRaw = ref<unknown>(null);
const historyError = computed<string | null>(() =>
  historyErrorRaw.value === null
    ? null
    : describeChannelError(historyErrorRaw.value),
);

/** toggleHistory opens/closes the panel, loading the history on open. */
async function toggleHistory(channel: NotificationChannel): Promise<void> {
  historyOpen.value = !historyOpen.value;
  if (historyOpen.value) {
    await loadHistory(channel);
  }
}

/** loadHistory reloads one channel's newest attempts. */
async function loadHistory(channel: NotificationChannel): Promise<void> {
  historyLoading.value = true;
  historyErrorRaw.value = null;
  try {
    deliveries.value = await channelsStore.history(channel.id);
  } catch (error) {
    historyErrorRaw.value = error;
    deliveries.value = [];
  } finally {
    historyLoading.value = false;
  }
}

/** handleToggle flips a channel's enabled flag in place. */
async function handleToggle(channel: NotificationChannel, enabled: boolean): Promise<void> {
  try {
    await channelsStore.update(channel.id, { enabled });
  } catch (error) {
    message.error(describeChannelError(error));
  }
}

/** handleDelete removes a channel after confirmation. */
async function handleDelete(channel: NotificationChannel): Promise<void> {
  try {
    await channelsStore.remove(channel.id);
    message.success(
      channelText("notifications.toast.deleted", "Deleted {name}", {
        name: channel.name,
      }),
    );
  } catch (error) {
    message.error(describeChannelError(error));
  }
}

/** handleTest delivers a synthetic event and records the outcome. */
async function handleTest(channel: NotificationChannel): Promise<void> {
  testing.value = true;
  testResult.value = null;
  testErrorRaw.value = null;
  try {
    testResult.value = await channelsStore.test(channel.id);
  } catch (error) {
    testErrorRaw.value = error;
  } finally {
    testing.value = false;
  }
  // A test leaves a history row; refresh an open panel so it appears.
  if (historyOpen.value) {
    await loadHistory(channel);
  }
}
</script>

<template>
  <NCard>
    <template #header>
      <NSpace align="center" :size="10">
        <NText strong>{{ props.channel.name }}</NText>
        <NTag size="small" round>{{ kindLabel(props.channel.kind) }}</NTag>
        <NTag size="small" round>
          {{ scopeLabel(props.channel, resolveResourceName) }}
        </NTag>
        <NTag
          :type="props.channel.enabled ? 'success' : 'default'"
          size="small"
          round
        >
          {{ props.channel.enabled ? $t("notifications.card.enabled") : $t("notifications.card.disabled") }}
        </NTag>
        <NTag
          v-if="props.channel.secrets_configured"
          size="small"
          round
          type="info"
        >
          {{ $t("notifications.card.secretConfigured") }}
        </NTag>
      </NSpace>
    </template>
    <template #header-extra>
      <NSpace align="center" :size="8">
        <NText depth="3" class="small">{{ $t("notifications.card.enabledLabel") }}</NText>
        <NSwitch
          :value="props.channel.enabled"
          :disabled="!canMutate"
          :aria-label="$t('notifications.card.enableAria', { name: props.channel.name })"
          @update:value="(value: boolean) => void handleToggle(props.channel, value)"
        />
      </NSpace>
    </template>

    <div class="channel-body" :data-channel="props.channel.name">
      <NSpace vertical :size="12">
        <NText depth="3" class="mono">{{ configSummary(props.channel) }}</NText>
        <NSpace :size="8" wrap>
          <NTag
            v-for="event in props.channel.events"
            :key="event"
            size="small"
            :bordered="false"
          >
            {{ eventLabel(event) }}
          </NTag>
        </NSpace>
        <NSpace align="center" :size="12" wrap>
          <NButton
            size="small"
            secondary
            :disabled="!canMutate"
            :loading="testing"
            @click="void handleTest(props.channel)"
          >
            {{ $t("notifications.card.sendTest") }}
          </NButton>
          <NText
            v-if="testFailure"
            type="error"
            depth="1"
            class="small"
            data-test-channel-result
          >
            {{ $t("notifications.card.testFailed") }}{{ testFailure }}
          </NText>
          <NText
            v-else-if="testResult"
            :type="testResult.ok ? 'success' : 'error'"
            depth="1"
            class="small"
            data-test-channel-result
          >
            {{ testResult.ok ? $t("notifications.card.testDelivered") : $t("notifications.card.testFailed") }}{{
              testResult.message
            }}
          </NText>
          <NText v-else depth="3" class="small">
            {{ $t("notifications.card.noTest") }}
          </NText>
        </NSpace>
        <div class="history" data-test-channel-history>
          <NButton
            size="small"
            text
            :loading="historyLoading"
            @click="void toggleHistory(props.channel)"
          >
            {{
              historyOpen
                ? $t("notifications.history.hide")
                : $t("notifications.history.show")
            }}
          </NButton>
          <template v-if="historyOpen">
            <NText
              v-if="historyError"
              type="error"
              depth="1"
              class="small"
              data-test-channel-history-error
            >
              {{ historyError }}
            </NText>
            <NText
              v-else-if="!historyLoading && deliveries.length === 0"
              depth="3"
              class="small"
            >
              {{ $t("notifications.history.empty") }}
            </NText>
            <ul v-else class="history-list">
              <li
                v-for="attempt in deliveries"
                :key="attempt.id"
                class="history-row"
                data-test-channel-history-row
              >
                <NSpace align="center" :size="8" wrap>
                  <NTag
                    size="tiny"
                    :bordered="false"
                    :type="attempt.status === 'success' ? 'success' : 'error'"
                  >
                    {{ deliveryStatusLabel(attempt.status) }}
                  </NTag>
                  <NText depth="3" class="small">
                    {{ deliveryEventLabel(attempt.event_type) }}
                  </NText>
                  <NText depth="3" class="small">
                    {{ relativeTime(attempt.created_at) }}
                  </NText>
                </NSpace>
                <NText
                  v-if="attempt.error_summary"
                  type="error"
                  depth="2"
                  class="small history-error"
                >
                  {{ attempt.error_summary }}
                </NText>
              </li>
            </ul>
          </template>
        </div>
        <NSpace align="center" :size="12">
          <NButton
            size="small"
            :disabled="!canMutate"
            @click="openEdit(props.channel)"
          >
            {{ $t("notifications.card.edit") }}
          </NButton>
          <NPopconfirm
            :positive-button-props="{ type: 'error' }"
            @positive-click="handleDelete(props.channel)"
          >
            <template #trigger>
              <NButton
                size="small"
                ghost
                type="error"
                :disabled="!canMutate"
              >
                {{ $t("notifications.card.delete") }}
              </NButton>
            </template>
            {{ $t("notifications.card.deleteConfirm", { name: props.channel.name }) }}
          </NPopconfirm>
          <NText depth="3" class="small">
            {{ $t("notifications.card.updated") }} {{ relativeTime(props.channel.updated_at) }}
          </NText>
        </NSpace>
      </NSpace>
    </div>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-xs);
}

.history {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  align-items: flex-start;
}

.history-list {
  list-style: none;
  margin: 0;
  padding: 0;
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.history-row {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.history-error {
  font-family: var(--font-mono);
  word-break: break-word;
}
</style>
