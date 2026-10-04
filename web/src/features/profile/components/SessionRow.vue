<script setup lang="ts">
import { NButton, NPopconfirm, NTag } from "naive-ui";
import { computed } from "vue";

import type { AuthSession } from "@/features/profile/schemas/sessions";
import { deviceLabel } from "@/features/profile/utils/deviceLabel";
import { formatDate, relativeTime } from "@/shared/utils/format";

interface Props {
  session: AuthSession;
  busy: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  signOut: [session: AuthSession];
}>();

const label = computed<string>(() => deviceLabel(props.session.user_agent));

const ip = computed<string>(() =>
  props.session.ip.trim() === "" ? "unknown" : props.session.ip,
);

const createdRelative = computed<string>(() =>
  relativeTime(props.session.created_at),
);
const activeRelative = computed<string>(() =>
  relativeTime(props.session.last_used_at),
);
const createdAbsolute = computed<string>(() =>
  formatDate(props.session.created_at),
);
const activeAbsolute = computed<string>(() =>
  formatDate(props.session.last_used_at),
);

/**
 * confirmText warns harder for the current session: ending it signs the
 * user out here, while other sessions end silently in the background.
 */
const confirmText = computed<string>(() =>
  props.session.current
    ? "Sign out this device? You will be signed out here and returned to the sign-in page."
    : `Sign out ${label.value}? That device will need to sign in again.`,
);
</script>

<template>
  <li class="session-row" :title="session.user_agent || undefined">
    <div class="session-main">
      <span class="session-label">
        {{ label }}
        <NTag v-if="session.current" size="small" type="success">This device</NTag>
      </span>
      <span class="small muted session-meta">
        {{ ip }} · Created
        <time :datetime="session.created_at" :title="createdAbsolute">
          {{ createdRelative }}
        </time>
        · Last active
        <time :datetime="session.last_used_at" :title="activeAbsolute">
          {{ activeRelative }}
        </time>
      </span>
    </div>
    <NPopconfirm
      :positive-button-props="session.current ? { type: 'error' } : undefined"
      positive-text="Sign out"
      negative-text="Keep"
      @positive-click="emit('signOut', session)"
    >
      <template #trigger>
        <NButton
          size="small"
          :type="session.current ? 'error' : 'default'"
          :loading="busy"
        >
          Sign out
        </NButton>
      </template>
      {{ confirmText }}
    </NPopconfirm>
  </li>
</template>

<style scoped>
.session-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) 0;
  border-bottom: 1px solid var(--border);
  list-style: none;
}

.session-row:last-child {
  border-bottom: none;
}

.session-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.session-label {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-meta {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.small {
  font-size: var(--text-xs);
}

@media (max-width: 640px) {
  .session-row {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
