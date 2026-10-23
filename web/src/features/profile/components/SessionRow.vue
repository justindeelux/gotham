<script setup lang="ts">
import { NButton, NPopconfirm, NTag } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { AuthSession } from "@/features/profile/schemas/sessions";
import { deviceLabel } from "@/features/profile/utils/deviceLabel";
import type { DeviceLocale } from "@/features/profile/utils/deviceLabel";
import { localeTag } from "@/shared/i18n";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  session: AuthSession;
  busy: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  signOut: [session: AuthSession];
}>();

const { t, locale } = useI18n();

const label = computed<string>(() =>
  deviceLabel(props.session.user_agent, locale.value as DeviceLocale),
);

const ip = computed<string>(() =>
  props.session.ip.trim() === "" ? t("profile.sessions.unknownIp") : props.session.ip,
);

const createdRelative = computed<string>(() =>
  relativeTime(props.session.created_at),
);
const activeRelative = computed<string>(() =>
  relativeTime(props.session.last_used_at),
);
const createdAbsolute = computed<string>(() =>
  formatDateTime(props.session.created_at),
);
const activeAbsolute = computed<string>(() =>
  formatDateTime(props.session.last_used_at),
);

function formatDateTime(iso: string): string {
  const time = new Date(iso);
  if (Number.isNaN(time.getTime())) {
    return t("profile.sessions.unknownTime");
  }
  return time.toLocaleString(localeTag(), {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const confirmText = computed<string>(() =>
  props.session.current
    ? t("profile.sessions.confirmCurrent")
    : t("profile.sessions.confirmOther", { label: label.value }),
);
</script>

<template>
  <li class="session-row" :title="session.user_agent || undefined">
    <div class="session-main">
      <span class="session-label">
        {{ label }}
        <NTag v-if="session.current" size="small" type="success">{{ t("profile.sessions.thisDevice") }}</NTag>
      </span>
      <span class="small muted session-meta">
        {{ ip }} · {{ t("profile.sessions.created") }}
        <time :datetime="session.created_at" :title="createdAbsolute">
          {{ createdRelative }}
        </time>
        · {{ t("profile.sessions.lastActive") }}
        <time :datetime="session.last_used_at" :title="activeAbsolute">
          {{ activeRelative }}
        </time>
      </span>
    </div>
    <NPopconfirm
      :positive-button-props="session.current ? { type: 'error' } : undefined"
      :positive-text="t('profile.sessions.signOut')"
      :negative-text="t('profile.sessions.keep')"
      placement="top-end"
      :style="{ maxWidth: 'min(26rem, calc(100vw - 3rem))' }"
      @positive-click="emit('signOut', session)"
    >
      <template #trigger>
        <NButton
          size="small"
          :type="session.current ? 'error' : 'default'"
          :loading="busy"
        >
          {{ t("profile.sessions.signOut") }}
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

@container (max-width: 480px) {
  .session-row {
    flex-direction: column;
    align-items: stretch;
  }

  .session-meta {
    white-space: normal;
    overflow: visible;
    overflow-wrap: anywhere;
  }
}
</style>
