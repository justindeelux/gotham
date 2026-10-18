<script setup lang="ts">
import { NAvatar, NButton, NDropdown, NSpace, NText } from "naive-ui";
import type { DropdownOption } from "naive-ui";
import { computed, h, onMounted, onUnmounted, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import { useAuthStore } from "@/features/auth";
import { useEditorStore } from "@/features/files/stores/editor";
import { useTeamsStore } from "@/features/teams";
import { roleReadRetryMs, shouldRetryRoleRead } from "@/features/teams";
import type { TeamRole } from "@/features/teams";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

defineProps<{ compact?: boolean }>();

const { t } = useI18n();
const authStore = useAuthStore();
const teamsStore = useTeamsStore();
const router = useRouter();

/** displayName is the footer name: display name, falling back to the email. */
const displayName = computed<string>(() => {
  const name = authStore.user?.display_name?.trim();
  if (name) {
    return name;
  }
  return authStore.user?.email ?? t("shell.signedIn");
});

const userInitial = computed<string>(() =>
  (displayName.value[0] ?? "?").toUpperCase(),
);

/**
 * hasDisplayName tells the header whether a real display name exists. Without
 * one displayName falls back to the email, so the email line is dropped —
 * otherwise the menu shows the address twice (JUS-197).
 */
const hasDisplayName = computed<boolean>(() => {
  const name = authStore.user?.display_name?.trim();
  return name !== undefined && name !== "";
});

/**
 * roleText derives the footer label from the caller's raw team role through
 * the shared common role catalog (reactive in the active locale). The
 * teams-owned meRoleLabel helper stays untouched (I18N-8 owns it); an
 * unknown future role falls back to the raw value, never a guessed label.
 * A platform admin additionally carries the instance-level flag, which reads
 * "Platform admin" so it cannot be confused with the team admin role.
 */
const roleText = computed<string>(() => {
  const role: TeamRole | null = teamsStore.activeTeam?.role ?? null;
  let base: string;
  if (role === null) {
    base = t("common.roles.member");
  } else if (role === "owner") {
    base = t("common.roles.owner");
  } else if (role === "admin") {
    base = t("common.roles.admin");
  } else if (role === "read_only") {
    base = t("common.roles.readOnly");
  } else {
    base = role;
  }
  if (authStore.isPlatformAdmin) {
    return `${base} · ${t("common.roles.platformAdmin")}`;
  }
  return base;
});

/**
 * loadRole reads the caller's teams for the footer label. A failed read
 * keeps the neutral fallback and retries on the next mount or account
 * change, plus a bounded retry (see shouldRetryRoleRead) so a transient
 * failure does not pin the neutral label until the sidebar remounts; a
 * fetch already in flight needs no retry because the label follows the store
 * reactively. A disabled teams feature (loaded with an empty list) keeps the
 * neutral fallback permanently. The sign-out path resets the teams store
 * (see the auth store), so a failed or stale read can never pin the
 * previous account's role here.
 */
let roleRetries = 0;
let roleRetryTimer: ReturnType<typeof setTimeout> | null = null;

function loadRole(): void {
  roleRetries = 0;
  void readRole();
}

async function readRole(): Promise<void> {
  cancelRoleRetry();
  await teamsStore.ensureTeams();
  if (
    !shouldRetryRoleRead({
      loaded: teamsStore.loaded,
      loading: teamsStore.loading,
      retries: roleRetries,
    })
  ) {
    if (teamsStore.loaded || teamsStore.loading) {
      roleRetries = 0;
    }
    return;
  }
  roleRetries += 1;
  roleRetryTimer = setTimeout(() => void readRole(), roleReadRetryMs);
}

/** cancelRoleRetry drops a pending retry so it cannot fire after unmount. */
function cancelRoleRetry(): void {
  if (roleRetryTimer !== null) {
    clearTimeout(roleRetryTimer);
    roleRetryTimer = null;
  }
}

onMounted(loadRole);
onUnmounted(cancelRoleRetry);

// A different signed-in account (or a team change elsewhere) re-reads the
// role even when the sidebar never remounts.
watch(
  () => authStore.user?.email,
  () => {
    loadRole();
  },
);

const accountOptions = computed<DropdownOption[]>(() => [
  {
    type: "render",
    key: "user-info",
    render: () => h("div", { class: "account-menu-header" }, [
      h(NAvatar, { round: true, size: 36, src: authStore.user?.avatar }, {
        default: () => authStore.user?.avatar ? null : userInitial.value,
        fallback: () => userInitial.value,
      }),
      h("div", { class: "account-menu-info" }, [
        h("strong", { class: "account-menu-name" }, displayName.value),
        ...(hasDisplayName.value
          ? [h("span", { class: "account-menu-email" }, authStore.user?.email)]
          : []),
        h("span", { class: "account-menu-role" }, roleText.value),
      ]),
    ]),
  },
  { type: "divider", key: "header-divider" },
  { label: t("shell.profile"), key: "profile" },
  { type: "divider", key: "footer-divider" },
  { label: t("shell.signOut"), key: "sign-out" },
]);

async function handleSelect(key: string | number): Promise<void> {
  if (key === "profile") {
    await router.push({ name: "profile" });
    return;
  }
  if (key !== "sign-out") {
    return;
  }
  // A user-initiated logout drops the editor buffers, so dirty tabs confirm
  // first through the shared editor dialog (staying aborts the logout).
  // Forced sign-outs bypass this path and reset directly.
  const editor = useEditorStore();
  if (editor.hasDirty && !(await editor.confirmDiscard())) {
    return;
  }
  try {
    await authStore.logout();
  } catch {
    // logout clears the local session in `finally`; a failed revoke must not
    // block the redirect or surface as an unhandled rejection (B3-3).
  }
  await router.push({ name: "login" });
}
</script>

<template>
  <NDropdown
    trigger="click"
    placement="bottom-end"
    :options="accountOptions"
    :menu-props="() => ({ style: 'min-width: 240px; max-width: min(320px, calc(100vw - 24px))' })"
    @select="handleSelect"
  >
    <NButton v-if="compact" quaternary circle :aria-label="t('shell.account')" aria-haspopup="menu">
      <template #icon>
        <NAvatar round :size="24" :src="authStore.user?.avatar">
          <template v-if="!authStore.user?.avatar">{{ userInitial }}</template>
          <template #fallback>{{ userInitial }}</template>
        </NAvatar>
      </template>
    </NButton>
    <NButton v-else quaternary class="me-card" :aria-label="t('shell.account')" aria-haspopup="menu">
      <NSpace align="center" :size="8" :wrap="false" :wrap-item="false" class="me-card-inner">
        <NAvatar round :size="24" :src="authStore.user?.avatar">
          <template v-if="!authStore.user?.avatar">{{ userInitial }}</template>
          <template #fallback>{{ userInitial }}</template>
        </NAvatar>
        <span class="me-meta">
          <NText class="me-email">{{ displayName }}</NText>
          <NText depth="3" class="me-role">{{ roleText }}</NText>
        </span>
        <GothamIcon name="chevron-down" class="me-chevron" />
      </NSpace>
    </NButton>
  </NDropdown>
</template>

<style scoped>
:global(.account-menu-header) {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3);
  color: var(--fg);
}

:global(.account-menu-info) {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: var(--space-1);
}

:global(.account-menu-name),
:global(.account-menu-email),
:global(.account-menu-role) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.account-menu-name) {
  color: var(--fg-2);
  font-size: var(--text-sm);
}

:global(.account-menu-email),
:global(.account-menu-role) {
  color: var(--muted);
  font-size: var(--text-xs);
}

.me-card {
  width: 100%;
  height: auto;
  padding: 5px 6px;
  border-radius: var(--radius-sm);
  justify-content: flex-start;
  text-align: left;
}

.me-card :deep(.n-button__content) {
  width: 100%;
  justify-content: flex-start;
}

.me-card:hover {
  background: var(--hover-row);
}

.me-card-inner {
  width: 100%;
  flex-wrap: nowrap;
  justify-content: flex-start;
}

.me-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  min-width: 0;
  flex: 1 1 auto;
  text-align: left;
  line-height: 1.3;
}

.me-email {
  font-size: var(--text-sm);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.me-role {
  font-size: var(--text-xs);
}

.me-chevron {
  width: 14px;
  height: 14px;
  flex: 0 0 auto;
  margin-left: auto;
  color: var(--muted);
}
</style>
