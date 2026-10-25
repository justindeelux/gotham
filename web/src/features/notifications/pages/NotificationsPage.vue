<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSpace,
  NSpin,
} from "naive-ui";
import { onMounted } from "vue";

import ChannelCard from "@/features/notifications/components/ChannelCard.vue";
import ChannelFormDialog from "@/features/notifications/components/ChannelFormDialog.vue";
import { provideChannelDialog } from "@/features/notifications/composables/useChannelDialog";
import { useNotificationsStore } from "@/features/notifications/stores/notifications";

const channelsStore = useNotificationsStore();
const { canMutate, loadResources, openCreate } = provideChannelDialog();

onMounted(async () => {
  await Promise.all([
    channelsStore.fetchChannels().catch(() => undefined),
    loadResources(),
  ]);
});
</script>

<template>
  <div class="notifications-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ $t("notifications.page.eyebrow") }}</p>
        <h1>{{ $t("notifications.page.title") }}</h1>
        <p class="page-desc">
          {{ $t("notifications.page.description") }}
        </p>
      </div>
      <div class="page-actions">
        <NButton
          v-if="canMutate"
          type="primary"
          :disabled="channelsStore.featureDisabled"
          @click="openCreate"
        >
          {{ $t("notifications.page.newChannel") }}
        </NButton>
      </div>
    </div>

    <NCard v-if="channelsStore.featureDisabled" :title="$t('notifications.page.unavailableTitle')">
      <NEmpty
        :description="$t('notifications.page.unavailableDesc')"
      />
    </NCard>

    <template v-else>
      <NAlert
        v-if="channelsStore.error"
        type="error"
        :show-icon="true"
        data-testid="channels-error"
      >
        {{ channelsStore.error }}
      </NAlert>

      <NSpin :show="channelsStore.loading">
        <NSpace vertical :size="16">
          <ChannelCard
            v-for="channel in channelsStore.channels"
            :key="channel.id"
            :channel="channel"
          />

          <NCard
            v-if="
              !channelsStore.loading &&
              channelsStore.loaded &&
              channelsStore.channels.length === 0 &&
              !channelsStore.error
            "
          >
            <NEmpty :description="$t('notifications.page.empty')">
              <template v-if="canMutate" #extra>
                <NButton type="primary" @click="openCreate">
                  {{ $t("notifications.page.createFirst") }}
                </NButton>
              </template>
            </NEmpty>
          </NCard>
        </NSpace>
      </NSpin>
    </template>

    <ChannelFormDialog />
  </div>
</template>

<style scoped>
.notifications-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
  }
}
</style>
