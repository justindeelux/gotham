<script setup lang="ts">
import { NButton, NCard, NPopconfirm, NSpace, NSwitch, NTag, NText, useMessage } from "naive-ui";
import { ref } from "vue";

import type {
  NotificationChannel,
  TestResult,
} from "@/features/notifications/api/notifications";
import {
  channelText,
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
const testResult = ref<TestResult | null>(null);

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
  try {
    testResult.value = await channelsStore.test(channel.id);
  } catch (error) {
    testResult.value = { ok: false, message: describeChannelError(error) };
  } finally {
    testing.value = false;
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
          :aria-label="`Enable ${props.channel.name}`"
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
            v-if="testResult"
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
</style>
