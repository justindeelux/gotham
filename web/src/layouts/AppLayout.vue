<script setup lang="ts">
import {
  NAvatar,
  NButton,
  NDropdown,
  NMenu,
  NSpace,
  NText,
} from "naive-ui";
import type { DropdownOption, MenuOption } from "naive-ui";
import { computed, ref, watch } from "vue";
import { RouterLink, RouterView, useRoute, useRouter } from "vue-router";

import ServerRail from "../components/ServerRail.vue";
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

const mobileNavOpen = ref(false);

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

/**
 * toggleNav drives the drawer on small screens and the collapsed sidebar
 * on desktop, matching the ≤1024px responsive behavior.
 */
function toggleNav(): void {
  if (window.matchMedia("(max-width: 1024px)").matches) {
    mobileNavOpen.value = !mobileNavOpen.value;
    return;
  }
  appStore.toggleSidebar();
}

watch(
  () => route.path,
  () => {
    mobileNavOpen.value = false;
  },
);
</script>

<template>
  <div
    class="app"
    :class="{
      'is-collapsed': appStore.sidebarCollapsed,
      'is-open': mobileNavOpen,
    }"
  >
    <ServerRail />

    <aside class="sidebar" aria-label="Product navigation">
      <div class="sidebar-head">Gotham</div>
      <div class="sidebar-body">
        <NMenu :value="activeKey" :options="menuOptions" @update:value="handleMenuSelect" />
      </div>
    </aside>

    <div class="main">
      <header class="topbar">
        <NButton
          quaternary
          circle
          class="nav-toggle"
          aria-label="Toggle navigation"
          @click="toggleNav"
        >
          <template #icon>
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linecap="round"
              stroke-linejoin="round"
              aria-hidden="true"
              width="18"
              height="18"
            >
              <path d="M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z" />
            </svg>
          </template>
        </NButton>
        <NText strong>{{ pageTitle }}</NText>
        <div class="topbar-right">
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
        </div>
      </header>

      <div class="view">
        <div class="page">
          <RouterView />
        </div>
      </div>
    </div>

    <div
      v-if="mobileNavOpen"
      class="backdrop"
      aria-hidden="true"
      @click="mobileNavOpen = false"
    ></div>
  </div>
</template>

<style scoped>
.app {
  display: grid;
  grid-template-columns: var(--rail-w) var(--sidebar-w) minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  height: 100vh;
  height: 100dvh;
  overflow: hidden;
}

.app.is-collapsed {
  grid-template-columns: var(--rail-w) 0 minmax(0, 1fr);
}

.sidebar {
  background: var(--surface);
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.is-collapsed .sidebar {
  visibility: hidden;
}

.sidebar-head {
  position: sticky;
  top: 0;
  z-index: 3;
  height: var(--topbar-h);
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0 var(--space-4);
  border-bottom: 1px solid var(--border);
  font-family: var(--font-display);
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--fg-2);
}

.sidebar-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: var(--space-3) var(--space-2) var(--space-4);
}

.main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  background: var(--bg);
}

.topbar {
  position: sticky;
  top: 0;
  flex: 0 0 auto;
  height: var(--topbar-h);
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 0 var(--space-4);
  border-bottom: 1px solid var(--border);
  background: color-mix(in oklab, var(--bg) 94%, transparent);
  backdrop-filter: blur(12px);
  z-index: 20;
}

.topbar-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-1);
}

.nav-toggle {
  display: none;
}

.view {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
}

.page {
  padding: var(--space-6) var(--space-6) var(--space-12);
  max-width: 1360px;
}

.backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: color-mix(in oklab, black 62%, transparent);
}

@media (max-width: 1024px) {
  .app,
  .app.is-collapsed {
    grid-template-columns: var(--rail-w) minmax(0, 1fr);
  }

  .sidebar {
    position: fixed;
    left: var(--rail-w);
    top: 0;
    bottom: 0;
    width: var(--sidebar-w);
    z-index: 90;
    border-right: 1px solid var(--border-soft);
    box-shadow: var(--elev-raised);
    visibility: hidden;
  }

  .is-collapsed .sidebar {
    visibility: hidden;
  }

  .app.is-open .sidebar {
    visibility: visible;
  }

  .nav-toggle {
    display: inline-flex;
  }

  .page {
    padding: var(--space-5) var(--space-5) var(--space-8);
  }
}

@media (max-width: 640px) {
  .app {
    --rail-w: 60px;
  }

  .page {
    padding: var(--space-4) var(--space-4) var(--space-8);
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.001ms !important;
    transition-duration: 0.001ms !important;
  }
}
</style>
