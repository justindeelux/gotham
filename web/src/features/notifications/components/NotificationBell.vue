<script setup lang="ts">
import { NBadge, NButton, NPopover } from "naive-ui";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import { getApplication } from "@/features/applications";
import { getDatabase } from "@/features/databases";
import { useAuthStore } from "@/features/auth";
import { useNotificationInboxStore } from "@/features/notifications/stores/inbox";
import type { NotificationItem } from "@/features/notifications/api/inbox";
import {
  notificationIcon,
  notificationLinkType,
  notificationText,
  notificationTone,
} from "@/features/notifications/utils/inbox";
import GothamIcon from "@/shared/ui/GothamIcon.vue";
import { activeLocale } from "@/shared/i18n";
import { relativeTime } from "@/shared/utils/format";

const { t } = useI18n();
const router = useRouter();
const authStore = useAuthStore();
const inbox = useNotificationInboxStore();

const open = ref(false);
const bellButton = ref<InstanceType<typeof NButton> | null>(null);
const pendingId = ref<string | null>(null);
const missingId = ref<string | null>(null);

const bellLabel = computed<string>(() =>
  inbox.unreadCount > 0
    ? t("notifications.inbox.bellUnread", { count: inbox.unreadCount })
    : t("notifications.inbox.bell"),
);

function syncUser(userId: string | undefined): void {
  if (userId) {
    inbox.connect(userId);
  }
}

onMounted(() => syncUser(authStore.user?.id));
watch(() => authStore.user?.id, syncUser);

function setOpen(show: boolean): void {
  open.value = show;
  if (show) {
    void inbox.markSeen().catch(() => undefined);
  }
}

function closeOnEscape(): void {
  if (!open.value) {
    return;
  }
  open.value = false;
  void nextTick(() => {
    const element = bellButton.value?.$el;
    if (element instanceof HTMLElement) {
      element.focus();
    }
  });
}

function timeText(iso: string): string {
  return relativeTime(iso, activeLocale.value, { long: true });
}

function isLink(item: NotificationItem): boolean {
  return notificationLinkType(item) !== null;
}

async function openItem(item: NotificationItem): Promise<void> {
  const linkType = notificationLinkType(item);
  if (!linkType || pendingId.value !== null) {
    return;
  }
  const itemId = item.id;
  pendingId.value = itemId;
  missingId.value = null;
  try {
    if (linkType === "application") {
      const application = await getApplication(item.resource_id ?? "");
      await router.push({
        name: "application-detail",
        params: {
          projectId: item.project_id || application.project_id,
          environmentId: application.environment_id,
          id: application.id,
        },
      });
    } else {
      const database = await getDatabase(item.resource_id ?? "");
      await router.push({
        name: "database-detail",
        params: {
          projectId: item.project_id || database.project_id,
          environmentId: database.environment_id,
          id: database.id,
        },
      });
    }
    open.value = false;
  } catch {
    missingId.value = itemId;
  } finally {
    pendingId.value = null;
  }
}
</script>

<template>
  <NPopover
    :show="open"
    trigger="click"
    placement="bottom-end"
    :duration="0"
    :animated="false"
    @update:show="setOpen"
  >
    <template #trigger>
      <NButton
        ref="bellButton"
        quaternary
        circle
        class="notif-bell"
        :aria-label="bellLabel"
        aria-haspopup="dialog"
        :aria-expanded="open"
        @keydown.esc.stop.prevent="closeOnEscape"
      >
        <template #icon>
          <NBadge
            class="notif-bell-badge"
            :value="inbox.unreadCount"
            :max="99"
            :show-zero="false"
            :aria-label="t('notifications.inbox.unreadCount', { count: inbox.unreadCount })"
          >
            <GothamIcon name="bell" />
          </NBadge>
        </template>
      </NButton>
    </template>

    <div
      class="notif-inbox"
      role="dialog"
      :aria-label="t('notifications.inbox.title')"
      @keydown.esc.stop.prevent="closeOnEscape"
    >
      <p class="notif-inbox-title">{{ t("notifications.inbox.title") }}</p>
      <p v-if="inbox.items.length === 0" class="notif-inbox-empty">
        {{ t("notifications.inbox.empty") }}
      </p>
      <ul v-else class="notif-inbox-list">
        <li v-for="item in inbox.items" :key="item.id" class="notif-inbox-row">
          <component
            :is="isLink(item) ? 'button' : 'div'"
            class="notif-inbox-item"
            :type="isLink(item) ? 'button' : undefined"
            :disabled="isLink(item) && pendingId === item.id ? true : undefined"
            @click="openItem(item)"
          >
            <GothamIcon
              :name="notificationIcon(item.event_key)"
              class="notif-inbox-icon"
              :class="`is-${notificationTone(item.event_key)}`"
              aria-hidden="true"
            />
            <span class="notif-inbox-body">
              <span class="notif-inbox-line">
                {{ notificationText(item.event_key, item) }}
              </span>
              <span v-if="item.error" class="notif-inbox-error">{{ item.error }}</span>
              <span v-if="missingId === item.id" class="notif-inbox-missing">
                {{ t("notifications.inbox.unavailable") }}
              </span>
              <time class="notif-inbox-time" :datetime="item.created_at">
                {{ timeText(item.created_at) }}
              </time>
            </span>
          </component>
        </li>
      </ul>
    </div>
  </NPopover>
</template>

<style scoped>
.notif-bell {
  color: var(--fg);
}

.notif-bell-badge :deep(.n-badge-sup) {
  font-size: var(--text-xs);
}

.notif-inbox {
  width: min(22rem, calc(100vw - 7rem));
  max-height: min(26rem, calc(100dvh - var(--space-12)));
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.notif-inbox-title {
  margin: 0;
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--fg-2);
}

.notif-inbox-empty {
  margin: 0;
  padding: var(--space-3) 0;
  font-size: var(--text-sm);
  color: var(--muted);
}

.notif-inbox-list {
  margin: 0;
  padding: 0;
  list-style: none;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.notif-inbox-row + .notif-inbox-row {
  border-top: 1px solid var(--border);
}

.notif-inbox-item {
  display: flex;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2) 0;
  border: 0;
  background: none;
  font: inherit;
  text-align: left;
  color: var(--fg);
}

button.notif-inbox-item {
  cursor: pointer;
}

button.notif-inbox-item:hover {
  background: var(--hover-row);
}

button.notif-inbox-item:focus-visible {
  outline: 2px solid var(--accent-ink);
  outline-offset: -2px;
}

.notif-inbox-icon {
  width: var(--space-4);
  height: var(--space-4);
  flex: 0 0 auto;
  margin-top: var(--space-1);
}

.notif-inbox-icon.is-success {
  color: var(--success);
}

.notif-inbox-icon.is-danger {
  color: var(--danger);
}

.notif-inbox-icon.is-neutral {
  color: var(--muted);
}

.notif-inbox-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.notif-inbox-line {
  font-size: var(--text-sm);
  color: var(--fg);
  overflow-wrap: anywhere;
}

.notif-inbox-error {
  font-size: var(--text-xs);
  color: var(--danger-ink);
  overflow-wrap: anywhere;
}

.notif-inbox-missing {
  font-size: var(--text-xs);
  color: var(--muted);
}

.notif-inbox-time {
  font-size: var(--text-xs);
  color: var(--muted);
}
</style>
