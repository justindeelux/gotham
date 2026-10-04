<script setup lang="ts">
import { NAvatar, NCard, NDescriptions, NDescriptionsItem } from "naive-ui";
import { computed } from "vue";

import { useAuthStore } from "@/features/auth";

const authStore = useAuthStore();

const displayName = computed<string>(() => {
  const name = authStore.user?.display_name?.trim();
  return name ? name : (authStore.user?.email ?? "Signed in");
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
  return isAdmin ? "Platform admin" : "Member";
});

/** memberSince renders created_at as a plain calendar date. */
const memberSince = computed<string>(() => {
  const raw = authStore.user?.created_at;
  if (!raw) {
    return "—";
  }
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) {
    return "—";
  }
  return date.toLocaleDateString(undefined, {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
});
</script>

<template>
  <NCard title="Account">
    <div class="identity-row">
      <NAvatar round :size="48" :src="authStore.user?.avatar">
        <template v-if="!authStore.user?.avatar">{{ userInitial }}</template>
        <template #fallback>{{ userInitial }}</template>
      </NAvatar>
      <div class="identity-meta">
        <strong class="identity-name">{{ displayName }}</strong>
        <span class="small muted">
          Your avatar comes from GitHub when you sign in with GitHub,
          otherwise your initials are shown.
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
        <NDescriptionsItem label="Email">{{ authStore.user?.email }}</NDescriptionsItem>
        <NDescriptionsItem label="Platform role">{{ platformRoleLabel }}</NDescriptionsItem>
        <NDescriptionsItem label="Member since">{{ memberSince }}</NDescriptionsItem>
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

/* Narrow containers stack each fact label-over-value. Naive renders one
 * table row per fact (header + content cells), so both cells go block. */
@container (max-width: 480px) {
  .identity-facts-wrap :deep(.n-descriptions-table-header),
  .identity-facts-wrap :deep(.n-descriptions-table-content) {
    display: block;
    width: 100%;
  }
}

.small {
  font-size: var(--text-xs);
}
</style>
