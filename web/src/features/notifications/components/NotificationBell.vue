<script setup lang="ts">
import { NBadge, NButton, NPopover, NTooltip } from "naive-ui";
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
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

const props = withDefaults(
  defineProps<{ variant?: "topbar" | "rail" }>(),
  { variant: "topbar" },
);

const { t } = useI18n();
const router = useRouter();
const authStore = useAuthStore();
const inbox = useNotificationInboxStore();

const isRail = computed<boolean>(() => props.variant === "rail");
const open = ref(false);
const bellButton = ref<InstanceType<typeof NButton> | null>(null);
const railButton = ref<HTMLButtonElement | null>(null);
const popoverContent = ref<HTMLElement | null>(null);
const pendingId = ref<string | null>(null);
const missingId = ref<string | null>(null);
let openFromKeyboard = false;

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
  const fromKeyboard = openFromKeyboard;
  openFromKeyboard = false;
  open.value = show;
  if (show) {
    void inbox.markSeen().catch(() => undefined);
    if (fromKeyboard) {
      void focusPopover();
    }
  }
}

function markKeyboardOpen(event: KeyboardEvent): void {
  if (event.key === "Enter" || event.key === " " || event.key === "Spacebar") {
    openFromKeyboard = true;
  }
}

function markMouseOpen(): void {
  openFromKeyboard = false;
}

async function focusPopover(attempt = 0): Promise<void> {
  await nextTick();
  const content = popoverContent.value;
  if (!content) {
    if (attempt < 10) {
      await new Promise<void>((resolve) => setTimeout(resolve, 0));
      await focusPopover(attempt + 1);
    }
    return;
  }
  const first = content.querySelector<HTMLElement>(
    "button.notif-inbox-item:not([disabled])",
  );
  (first ?? content).focus({ preventScroll: true });
}

function popoverFocusables(content: HTMLElement): HTMLElement[] {
  return Array.from(
    content.querySelectorAll<HTMLElement>(
      "button:not([disabled]), [href], input:not([disabled]), " +
        "select:not([disabled]), textarea:not([disabled]), " +
        "[tabindex]:not([tabindex='-1'])",
    ),
  );
}

function onPopoverKeydown(event: KeyboardEvent): void {
  if (event.key !== "Tab") {
    return;
  }
  const content = popoverContent.value;
  if (!content) {
    return;
  }
  const focusables = popoverFocusables(content);
  if (focusables.length === 0) {
    event.preventDefault();
    content.focus({ preventScroll: true });
    return;
  }
  const first = focusables[0];
  const last = focusables[focusables.length - 1];
  const active = document.activeElement;
  const inside = active instanceof HTMLElement && content.contains(active);
  if (event.shiftKey) {
    if (!inside || active === first) {
      event.preventDefault();
      last.focus({ preventScroll: true });
    }
    return;
  }
  if (!inside || active === last) {
    event.preventDefault();
    first.focus({ preventScroll: true });
  }
}

function closeFromEscape(): void {
  if (!open.value) {
    return;
  }
  open.value = false;
  void nextTick(() => {
    const element = isRail.value ? railButton.value : bellButton.value?.$el;
    if (element instanceof HTMLElement) {
      element.focus();
    }
  });
}

function onDocumentKeydown(event: KeyboardEvent): void {
  if (event.key !== "Escape") {
    return;
  }
  closeFromEscape();
}

watch(open, (value) => {
  if (typeof document === "undefined") {
    return;
  }
  if (value) {
    document.addEventListener("keydown", onDocumentKeydown);
    return;
  }
  document.removeEventListener("keydown", onDocumentKeydown);
});

onUnmounted(() => {
  if (typeof document !== "undefined") {
    document.removeEventListener("keydown", onDocumentKeydown);
  }
});

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
    :placement="isRail ? 'right-end' : 'bottom-end'"
    :duration="0"
    :animated="false"
    @update:show="setOpen"
  >
    <template #trigger>
      <span v-if="isRail" class="notif-rail-target">
        <NTooltip placement="right" :disabled="open">
          <template #trigger>
            <button
              ref="railButton"
              type="button"
              class="rail-btn notif-rail-bell"
              :aria-label="bellLabel"
              aria-haspopup="dialog"
              :aria-expanded="open"
              @keydown="markKeyboardOpen"
              @mousedown="markMouseOpen"
            >
              <GothamIcon name="bell" />
              <span
                v-if="inbox.unreadCount > 0"
                class="rail-badge"
                aria-hidden="true"
              >{{ inbox.unreadCount > 9 ? "9+" : inbox.unreadCount }}</span>
            </button>
          </template>
          <span>{{ t("notifications.inbox.title") }}</span>
        </NTooltip>
      </span>
      <NButton
        v-else
        ref="bellButton"
        quaternary
        circle
        class="notif-bell"
        :aria-label="bellLabel"
        aria-haspopup="dialog"
        :aria-expanded="open"
        @keydown="markKeyboardOpen"
        @mousedown="markMouseOpen"
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
      ref="popoverContent"
      class="notif-inbox"
      role="dialog"
      tabindex="-1"
      :aria-label="t('notifications.inbox.title')"
      @keydown="onPopoverKeydown"
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
              <span v-if="item.error && item.event_key !== 'redeploy_needed'" class="notif-inbox-error">{{ item.error }}</span>
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

.notif-rail-target {
  display: inline-flex;
}

.notif-rail-bell {
  border: 0;
  padding: 0;
  font: inherit;
  cursor: pointer;
}

.notif-inbox-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2);
  border: 0;
  border-radius: var(--radius-sm);
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

.notif-inbox-icon.is-warning {
  color: var(--warn);
}

.notif-inbox-body {
  display: flex;
  flex: 1 1 auto;
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
