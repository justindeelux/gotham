<script setup lang="ts">
import {
  NAvatar,
  NButton,
  NDropdown,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NSpace,
  NText,
} from "naive-ui";
import type { DropdownOption, MenuOption } from "naive-ui";
import { computed } from "vue";
import { RouterLink, RouterView, useRoute, useRouter } from "vue-router";

import { useAppStore } from "../stores/app";
import { useAuthStore } from "../stores/auth";

const appStore = useAppStore();
const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();

const menuOptions: MenuOption[] = [
  { label: "Dashboard", key: "dashboard" },
  { label: "Servers", key: "servers" },
];

const accountOptions: DropdownOption[] = [{ label: "Sign out", key: "sign-out" }];

const activeKey = computed<string>(() => String(route.name ?? "dashboard"));

const pageTitle = computed<string>(() => route.meta.title ?? "Gotham");

const userLabel = computed<string>(() => authStore.user?.email ?? "");

const userInitial = computed<string>(() =>
  (authStore.user?.email?.[0] ?? "?").toUpperCase(),
);

function handleMenuSelect(key: string | number): void {
  void router.push({ name: String(key) });
}

async function handleAccountSelect(key: string | number): Promise<void> {
  if (key !== "sign-out") {
    return;
  }
  await authStore.logout();
  await router.push({ name: "login" });
}
</script>

<template>
  <NLayout class="shell" has-sider>
    <NLayoutSider
      bordered
      collapse-mode="width"
      :collapsed="appStore.sidebarCollapsed"
      :collapsed-width="64"
      :width="240"
      show-trigger
      @update:collapsed="appStore.setSidebarCollapsed"
    >
      <div class="brand">Gotham</div>
      <NMenu
        :value="activeKey"
        :options="menuOptions"
        :collapsed="appStore.sidebarCollapsed"
        :collapsed-width="64"
        :collapsed-icon-size="20"
        @update:value="handleMenuSelect"
      />
    </NLayoutSider>

    <NLayout>
      <NLayoutHeader class="topbar" bordered>
        <NText strong>{{ pageTitle }}</NText>
        <NSpace align="center">
          <NDropdown
            v-if="authStore.isAuthenticated"
            trigger="click"
            :options="accountOptions"
            @select="handleAccountSelect"
          >
            <NButton quaternary>
              <NSpace align="center" :size="8">
                <NAvatar round :size="28" :src="authStore.user?.avatar">
                  {{ userInitial }}
                </NAvatar>
                <NText depth="2">{{ userLabel }}</NText>
              </NSpace>
            </NButton>
          </NDropdown>
          <RouterLink v-else to="/login">
            <NButton quaternary type="primary">Sign in</NButton>
          </RouterLink>
        </NSpace>
      </NLayoutHeader>

      <NLayoutContent class="content" content-style="padding: 24px;">
        <RouterView />
      </NLayoutContent>
    </NLayout>
  </NLayout>
</template>

<style scoped>
.shell {
  height: 100vh;
}

.brand {
  display: flex;
  align-items: center;
  height: 56px;
  padding: 0 20px;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.02em;
}

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 56px;
  padding: 0 20px;
}

.content {
  height: calc(100vh - 56px);
}
</style>
