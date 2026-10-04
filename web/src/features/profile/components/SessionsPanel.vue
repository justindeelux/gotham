<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NPopconfirm,
  NSpin,
} from "naive-ui";
import { onMounted } from "vue";

import SessionRow from "@/features/profile/components/SessionRow.vue";
import { useSessionsPanel } from "@/features/profile/composables/useSessionsPanel";
import { profileMessages } from "@/features/profile/schemas/profile";

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

onMounted(() => {
  void load();
});
</script>

<template>
  <NCard title="Active sessions">
    <!-- Panel-width container for the row stack rule (JUS-19 convention). -->
    <div class="sessions-panel">
      <p class="small muted panel-desc">
        {{ profileMessages.sessionsIntro }}
      </p>

      <div v-if="loading && !loaded" class="panel-center">
        <NSpin aria-label="Loading sessions" />
      </div>

      <template v-else>
        <NAlert v-if="errorMessage" type="error" :show-icon="true">
          {{ errorMessage }}
        </NAlert>
        <div v-if="errorMessage" class="panel-actions">
          <NButton size="small" @click="load">{{ profileMessages.actionRetry }}</NButton>
        </div>

        <NEmpty
          v-if="loaded && sessions.length === 0"
          :description="profileMessages.sessionsEmpty"
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
            {{ profileMessages.needsReauth }}
          </NAlert>
          <div class="panel-actions">
            <NButton size="small" @click="signOutHere">
              {{ profileMessages.signInAgain }}
            </NButton>
          </div>
        </template>

        <div class="panel-actions">
          <NPopconfirm
            positive-text="Sign out others"
            negative-text="Keep"
            @positive-click="endOtherSessions"
          >
            <template #trigger>
              <NButton :disabled="others.length === 0" :loading="revokingOthers">
                Sign out all other devices
              </NButton>
            </template>
            Sign out {{ others.length }} other
            {{ others.length === 1 ? "session" : "sessions" }}? Those devices
            will need to sign in again.
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
