<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import { useAuthStore } from "@/features/auth";
import { useServersStore } from "@/features/servers";
import MeCard from "./MeCard.vue";
import { activeNavKey, navSections, visibleNavSections } from "./navigation";
import { useAppVersion } from "./useAppVersion";
import { useMobileNav } from "./useMobileNav";

const { t } = useI18n();
const serversStore = useServersStore();
const authStore = useAuthStore();
const route = useRoute();
const { sidebarRef } = useMobileNav();
const { versionTag } = useAppVersion();

const serversCount = computed<number>(() => serversStore.servers.length);

const activeKey = computed<string>(() => activeNavKey(route.path));

const sections = computed(() => visibleNavSections(navSections, authStore.isOwner));

const activeGroupKey = computed<string | null>(() => {
  for (const section of sections.value) {
    for (const item of section.items) {
      if (item.children?.some((child) => child.key === activeKey.value)) {
        return item.key;
      }
    }
  }
  return null;
});

const expandedGroups = ref<Set<string>>(new Set());

watch(
  activeGroupKey,
  (key) => {
    if (key && !expandedGroups.value.has(key)) {
      expandedGroups.value = new Set([...expandedGroups.value, key]);
    }
  },
  { immediate: true },
);

function isGroupOpen(key: string): boolean {
  return expandedGroups.value.has(key);
}

function toggleGroup(key: string): void {
  const next = new Set(expandedGroups.value);
  if (next.has(key)) {
    next.delete(key);
  } else {
    next.add(key);
  }
  expandedGroups.value = next;
}

function groupPanelId(key: string): string {
  return `nav-group-${key}`;
}
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
      <img class="brand" src="/logo/gotham-wordmark.svg" alt="Gotham" />
      <span v-if="versionTag" class="tag" :title="`Control plane ${versionTag}`">{{
        versionTag
      }}</span>
    </div>
    <nav class="sidebar-body">
      <template v-for="section in sections" :key="section.labelKey">
        <p class="nav-label">{{ t(section.labelKey) }}</p>
        <template v-for="item in section.items" :key="item.key">
          <div v-if="item.children" class="nav-group">
            <button
              type="button"
              class="nav-item nav-group-toggle"
              :class="{ 'is-open': isGroupOpen(item.key) }"
              :aria-expanded="isGroupOpen(item.key)"
              :aria-controls="groupPanelId(item.key)"
              @click="toggleGroup(item.key)"
            >
              <GothamIcon :name="item.icon" />
              <span>{{ t(item.labelKey) }}</span>
              <GothamIcon
                class="nav-chevron"
                name="chevron-down"
                aria-hidden="true"
              />
            </button>
            <div
              v-show="isGroupOpen(item.key)"
              :id="groupPanelId(item.key)"
              class="nav-subitems"
            >
              <RouterLink
                v-for="child in item.children"
                :key="child.key"
                class="nav-item nav-subitem"
                :class="{ 'is-active': activeKey === child.key }"
                :to="{ name: child.to }"
              >
                <GothamIcon :name="child.icon" />
                <span>{{ t(child.labelKey) }}</span>
              </RouterLink>
            </div>
          </div>
          <RouterLink
            v-else
            class="nav-item"
            :class="{ 'is-active': activeKey === item.key }"
            :to="{ name: item.to }"
          >
            <GothamIcon :name="item.icon" />
            <span>{{ t(item.labelKey) }}</span>
            <span v-if="item.key === 'servers'" class="nav-count">{{ serversCount }}</span>
          </RouterLink>
        </template>
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
  display: block;
  height: var(--brand-wordmark-h);
  width: auto;
  max-width: 100%;
  min-width: 0;
  flex: 0 1 auto;
  aspect-ratio: 946 / 232;
  object-fit: contain;
  object-position: left center;
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

button.nav-item {
  appearance: none;
  border: 0;
  background: transparent;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
}

.nav-item :deep(svg) {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  color: var(--meta);
}

a.nav-item:hover,
button.nav-item:hover {
  background: var(--hover-row);
  color: var(--fg-2);
}

a.nav-item:hover :deep(svg),
button.nav-item:hover :deep(svg) {
  color: var(--fg);
}

a.nav-item.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
}

a.nav-item.is-active :deep(svg) {
  color: var(--fg-2);
}

button.nav-item.is-open {
  color: var(--fg-2);
}

button.nav-item.is-open :deep(svg) {
  color: var(--fg-2);
}

.nav-chevron {
  width: 14px;
  height: 14px;
  margin-left: auto;
  transition: transform var(--motion-base) var(--ease-standard);
}

button.nav-item.is-open .nav-chevron {
  transform: rotate(180deg);
}

.nav-subitems {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.nav-subitem {
  padding-left: var(--space-6);
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
    left: 0;
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
  .nav-item {
    min-height: 44px;
  }
}
</style>
