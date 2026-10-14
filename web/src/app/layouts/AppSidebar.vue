<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import { useAuthStore } from "@/features/auth";
import { useServersStore } from "@/features/servers";
import MeCard from "./MeCard.vue";
import { activeNavKey, navSections } from "./navigation";
import { useAppVersion } from "./useAppVersion";
import { useMobileNav } from "./useMobileNav";

const { t } = useI18n();
const serversStore = useServersStore();
const authStore = useAuthStore();
const route = useRoute();
const { sidebarRef } = useMobileNav();
const { versionTag } = useAppVersion();

// Live count: the only pill backed by a store. Every other section has no
// backend yet, so no pill is rendered rather than a fabricated number.
const serversCount = computed<number>(() => serversStore.servers.length);

const activeKey = computed<string>(() => activeNavKey(route.path));
</script>

<template>
  <aside
    id="app-nav"
    ref="sidebarRef"
    class="sidebar"
    :aria-label="t('nav.label')"
    tabindex="-1"
  >
    <div class="sidebar-head">
      <span class="brand">Gotham</span>
      <span v-if="versionTag" class="tag" :title="`Control plane ${versionTag}`">{{
        versionTag
      }}</span>
    </div>
    <nav class="sidebar-body">
      <template v-for="section in navSections" :key="section.labelKey">
        <p class="nav-label">{{ t(section.labelKey) }}</p>
        <RouterLink
          v-for="item in section.items.filter((i) => !i.adminOnly || authStore.isPlatformAdmin)"
          :key="item.key"
          class="nav-item"
          :class="{ 'is-active': activeKey === item.key }"
          :to="{ name: item.to }"
        >
          <GothamIcon :name="item.icon" />
          <span>{{ t(item.labelKey) }}</span>
          <span v-if="item.key === 'servers'" class="nav-count">{{ serversCount }}</span>
        </RouterLink>
      </template>
    </nav>
    <div class="sidebar-foot">
      <MeCard />
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  background: var(--surface);
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
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

.brand {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  background: var(--surface);
  border: 1px solid var(--border);
  color: var(--muted);
  margin-left: auto;
  flex: 0 0 auto;
}

.sidebar-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: var(--space-3) var(--space-2) var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.nav-label {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--muted);
  padding: var(--space-3) var(--space-2) 3px;
  margin: 0;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: 6px var(--space-2);
  min-height: 30px;
  border-radius: var(--radius-sm);
  color: var(--muted);
  font-size: var(--text-sm);
  font-weight: 500;
  text-decoration: none;
  transition:
    background var(--motion-base) var(--ease-standard),
    color var(--motion-base) var(--ease-standard);
}

.nav-item :deep(svg) {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  color: var(--meta);
}

a.nav-item:hover {
  background: var(--hover-row);
  color: var(--fg-2);
}

a.nav-item:hover :deep(svg) {
  color: var(--fg);
}

a.nav-item.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
}

a.nav-item.is-active :deep(svg) {
  color: var(--fg-2);
}

.nav-item .nav-count {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  background: var(--surface-warm);
  border-radius: var(--radius-pill);
  padding: 1px 6px;
}

a.nav-item.is-active .nav-count {
  color: var(--fg);
}

.sidebar-foot {
  flex: 0 0 auto;
  position: sticky;
  bottom: 0;
  z-index: 2;
  padding: var(--space-2);
  border-top: 1px solid var(--border);
  background: var(--surface-warm);
}

@media (max-width: 1024px) {
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

  .app.is-open .sidebar {
    visibility: visible;
  }
}

@media (max-width: 640px) {
  /* Phone: every nav entry is a 44px touch target (B3-13). */
  .nav-item {
    min-height: 44px;
  }
}
</style>
