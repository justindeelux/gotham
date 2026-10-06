<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NPopconfirm,
  NSpin,
} from "naive-ui";
import { computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";

import SessionRow from "@/features/profile/components/SessionRow.vue";
import { useSessionsPanel } from "@/features/profile/composables/useSessionsPanel";

const { t } = useI18n();
const {
  sessions,
  others,
  loading,
  loaded,
  errorMessage,
  revokingId,
  revokingOthers,
  needsReauth,
  load,
  endSession,
  endOtherSessions,
  signOutHere,
} = useSessionsPanel();

/**
 * othersConfirmText names the destructive count with whole-sentence keys
 * (one vs many) instead of plural syntax, so both catalogs stay in parity.
 */
const othersConfirmText = computed<string>(() =>
  others.value.length === 1
    ? t("profile.sessions.confirmOthersOne", { count: others.value.length })
    : t("profile.sessions.confirmOthersMany", { count: others.value.length }),
);

onMounted(() => {
  void load();
});
</script>

<template>
  <NCard :title="t('profile.sessions.title')">
    <!-- Panel-width container for the row stack rule (JUS-19 convention). -->
    <div class="sessions-panel">
      <p class="small muted panel-desc">
        {{ t("profile.sessions.intro") }}
      </p>

      <div v-if="loading && !loaded" class="panel-center">
        <NSpin :aria-label="t('profile.sessions.loading')" />
      </div>

      <template v-else>
        <NAlert v-if="errorMessage" type="error" :show-icon="true">
          {{ errorMessage }}
        </NAlert>
        <div v-if="errorMessage" class="panel-actions">
          <NButton size="small" @click="load">{{ t("profile.sessions.retry") }}</NButton>
        </div>

        <NEmpty
          v-if="loaded && sessions.length === 0"
          :description="t('profile.sessions.empty')"
        />

        <ul v-if="sessions.length > 0" class="session-list">
          <SessionRow
            v-for="session in sessions"
            :key="session.id"
            :session="session"
            :busy="revokingId === session.id"
            @sign-out="endSession"
          />
        </ul>

        <template v-if="needsReauth">
          <NAlert type="warning" :show-icon="true" class="reauth-note">
            {{ t("profile.sessions.needsReauth") }}
          </NAlert>
          <div class="panel-actions">
            <NButton size="small" @click="signOutHere">
              {{ t("profile.sessions.signInAgain") }}
            </NButton>
          </div>
        </template>

        <div class="panel-actions">
          <NPopconfirm
            :positive-text="t('profile.sessions.confirmOthersPositive')"
            :negative-text="t('profile.sessions.keep')"
            @positive-click="endOtherSessions"
          >
            <template #trigger>
              <NButton :disabled="others.length === 0" :loading="revokingOthers">
                {{ t("profile.sessions.signOutOthers") }}
              </NButton>
            </template>
            {{ othersConfirmText }}
          </NPopconfirm>
        </div>
      </template>
    </div>
  </NCard>
</template>

<style scoped>
.sessions-panel {
  container-type: inline-size;
}

.panel-desc {
  margin-bottom: var(--space-3);
}

.panel-center {
  display: flex;
  justify-content: center;
  padding: var(--space-4) 0;
}

.session-list {
  margin: 0;
  padding: 0;
}

.panel-actions {
  margin-top: var(--space-3);
}

.reauth-note {
  margin-top: var(--space-3);
}

.small {
  font-size: var(--text-xs);
}
</style>
