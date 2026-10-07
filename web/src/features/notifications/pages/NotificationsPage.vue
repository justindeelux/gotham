<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSpace,
  NSpin,
} from "naive-ui";
import { onMounted, watch } from "vue";

import ChannelCard from "@/features/notifications/components/ChannelCard.vue";
import ChannelFormDialog from "@/features/notifications/components/ChannelFormDialog.vue";
import TeamScopeCard from "@/features/notifications/components/TeamScopeCard.vue";
import { provideChannelDialog } from "@/features/notifications/composables/useChannelDialog";
import { useNotificationsStore } from "@/features/notifications/stores/notifications";
import { useTeamsStore } from "@/features/teams";

/**
 * FE-8.1 (3) — notification channels.
 *
 * There is no mockup for this page; the layout follows the established
 * settings/form pattern and the channel cards of
 * docs/design/team-settings.html (which is the notifications reference).
 * Secrets are write-only: reads show the masked value the API returns, and an
 * update only carries a value the operator actually typed.
 *
 * Thin route component: team scope, channel cards and the channel dialog
 * live in their own components; the draft and resource loading live in
 * useChannelDialog.
 */
const channelsStore = useNotificationsStore();
const teamsStore = useTeamsStore();
// Per page instance: the cards and the dialog share this draft via inject,
// and it is dropped on unmount — typed secrets never survive a route change.
const { canMutate, loadResources, openCreate } = provideChannelDialog();

watch(
  () => teamsStore.activeTeamId,
  () => {
    void channelsStore.fetchChannels().catch(() => undefined);
    void loadResources(teamsStore.activeTeamId);
  },
);

onMounted(async () => {
  const selectionBeforeLoad = teamsStore.activeTeamId;
  await teamsStore.ensureTeams();
  // The selection watcher already reloaded the list when ensureTeams changed
  // the active team; only an unchanged selection needs an explicit first read.
  if (teamsStore.activeTeamId === selectionBeforeLoad) {
    await Promise.all([
      channelsStore.fetchChannels().catch(() => undefined),
      loadResources(teamsStore.activeTeamId),
    ]);
  }
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
      <TeamScopeCard v-if="!teamsStore.featureDisabled" />

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
