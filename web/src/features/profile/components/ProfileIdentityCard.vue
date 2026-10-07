<script setup lang="ts">
import { NAvatar, NCard, NDescriptions, NDescriptionsItem } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import { localeTag } from "@/shared/i18n";

const { t } = useI18n();
const authStore = useAuthStore();

const displayName = computed<string>(() => {
  const name = authStore.user?.display_name?.trim();
  return name ? name : (authStore.user?.email ?? t("profile.identity.signedIn"));
});

const userInitial = computed<string>(() =>
  (displayName.value[0] ?? "?").toUpperCase(),
);

/**
 * platformRoleLabel names the platform role: "admin" marks the first
 * account (the platform admin), every other account is a member. Team
 * roles are per-team and shown on the Teams page, not here.
 */
const platformRoleLabel = computed<string>(() => {
  const isAdmin = authStore.user?.is_platform_admin ?? authStore.user?.role === "admin";
  return isAdmin ? t("profile.identity.admin") : t("profile.identity.member");
});

/** memberSince renders created_at as a calendar date in the active locale. */
const memberSince = computed<string>(() => {
  const raw = authStore.user?.created_at;
  if (!raw) {
    return "—";
  }
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) {
    return "—";
  }
  return date.toLocaleDateString(localeTag(), {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
});
</script>

<template>
  <NCard :title="t('profile.identity.title')">
    <div class="identity-row">
      <NAvatar round :size="48" :src="authStore.user?.avatar">
        <template v-if="!authStore.user?.avatar">{{ userInitial }}</template>
        <template #fallback>{{ userInitial }}</template>
      </NAvatar>
      <div class="identity-meta">
        <strong class="identity-name">{{ displayName }}</strong>
        <span class="small muted">
          {{ t("profile.identity.avatarNote") }}
        </span>
      </div>
    </div>
    <div class="identity-facts-wrap">
      <NDescriptions
        :column="1"
        bordered
        label-placement="left"
        class="identity-facts"
      >
        <NDescriptionsItem :label="t('profile.identity.email')">{{ authStore.user?.email }}</NDescriptionsItem>
        <NDescriptionsItem :label="t('profile.identity.platformRole')">{{ platformRoleLabel }}</NDescriptionsItem>
        <NDescriptionsItem :label="t('profile.identity.memberSince')">{{ memberSince }}</NDescriptionsItem>
      </NDescriptions>
    </div>
  </NCard>
</template>

<style scoped>
.identity-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}

.identity-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.identity-name {
  font-size: var(--text-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.identity-facts-wrap {
  container-type: inline-size;
  max-width: 560px;
}

/* Narrow containers stack each fact label-over-value: the table, its body,
 * every row and both cells all go block, so no anonymous table boxes remain
 * to squeeze the value column. Naive's own display:table-cell rule chains
 * five classes (0,5,0); repeating its ancestor chain after this wrapper wins
 * (0,7,0) without !important. */
@container (max-width: 480px) {
  .identity-facts-wrap :deep(.n-descriptions-table),
  .identity-facts-wrap :deep(tbody),
  .identity-facts-wrap :deep(tr.n-descriptions-table-row) {
    display: block;
    width: 100%;
  }

  .identity-facts-wrap
    :deep(
      .n-descriptions .n-descriptions-table-wrapper .n-descriptions-table .n-descriptions-table-row .n-descriptions-table-header
    ),
  .identity-facts-wrap
    :deep(
      .n-descriptions .n-descriptions-table-wrapper .n-descriptions-table .n-descriptions-table-row .n-descriptions-table-content
    ) {
    display: block;
    width: 100%;
  }
}

.small {
  font-size: var(--text-xs);
}
</style>
