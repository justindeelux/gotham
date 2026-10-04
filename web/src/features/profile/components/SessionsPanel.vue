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
    <p class="small muted panel-desc">
      Every device signed in to your account. Ending a session signs that
      device out; ending this device signs you out here.
    </p>

    <div v-if="loading && !loaded" class="panel-center">
      <NSpin aria-label="Loading sessions" />
    </div>

    <template v-else>
      <template v-if="errorMessage">
        <NAlert type="error" :show-icon="true">
          {{ errorMessage }}
        </NAlert>
        <div class="panel-actions">
          <NButton size="small" @click="load">Retry</NButton>
        </div>
      </template>

      <NEmpty
        v-else-if="sessions.length === 0"
        description="No active sessions."
      />

      <ul v-else class="session-list">
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
          <NButton size="small" @click="signOutHere">Sign in again</NButton>
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
  </NCard>
</template>

<style scoped>
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
