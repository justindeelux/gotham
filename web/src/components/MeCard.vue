<script setup lang="ts">
import { NAvatar, NButton, NDropdown, NSpace, NText } from "naive-ui";
import type { DropdownOption } from "naive-ui";
import { computed } from "vue";
import { useRouter } from "vue-router";

import { useAuthStore } from "../stores/auth";
import GothamIcon from "./GothamIcon.vue";

const authStore = useAuthStore();
const router = useRouter();

const accountOptions: DropdownOption[] = [{ label: "Sign out", key: "sign-out" }];

const userEmail = computed<string>(() => authStore.user?.email ?? "Signed in");

const userInitial = computed<string>(() =>
  (authStore.user?.email?.[0] ?? "?").toUpperCase(),
);

async function handleSelect(key: string | number): Promise<void> {
  if (key !== "sign-out") {
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
  <NDropdown trigger="click" :options="accountOptions" @select="handleSelect">
    <NButton quaternary class="me-card" aria-label="Account">
      <NSpace align="center" :size="8" :wrap="false" class="me-card-inner">
        <NAvatar round :size="24" :src="authStore.user?.avatar">
          <template v-if="!authStore.user?.avatar">{{ userInitial }}</template>
          <template #fallback>{{ userInitial }}</template>
        </NAvatar>
        <span class="me-meta">
          <NText class="me-email">{{ userEmail }}</NText>
          <NText depth="3" class="me-role">Workspace member</NText>
        </span>
        <GothamIcon name="chevron-down" class="me-chevron" />
      </NSpace>
    </NButton>
  </NDropdown>
</template>

<style scoped>
.me-card {
  width: 100%;
  height: auto;
  padding: 5px 6px;
  border-radius: var(--radius-sm);
}

.me-card:hover {
  background: var(--hover-row);
}

.me-card-inner {
  width: 100%;
  flex-wrap: nowrap;
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
  color: var(--muted);
}
</style>
