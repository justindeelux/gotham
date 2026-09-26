<script setup lang="ts">
import {
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NSpace,
  NText,
} from "naive-ui";
import type { MenuOption } from "naive-ui";
import { computed } from "vue";
import { RouterView, useRoute, useRouter } from "vue-router";

import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const route = useRoute();
const router = useRouter();

const menuOptions: MenuOption[] = [{ label: "Dashboard", key: "dashboard" }];

const activeKey = computed<string>(() => String(route.name ?? "dashboard"));

const pageTitle = computed<string>(() => route.meta.title ?? "Gotham");

function handleMenuSelect(key: string | number): void {
  void router.push({ name: String(key) });
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
          <NText depth="3">guest</NText>
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
