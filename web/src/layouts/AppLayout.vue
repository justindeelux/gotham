<script setup lang="ts">
import { NAvatar, NButton, NDropdown, NInput, NTooltip } from "naive-ui";
import type { DropdownOption } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { RouterLink, RouterView, useRoute, useRouter } from "vue-router";
import { version as appVersion } from "../../package.json";

import GothamIcon from "../components/GothamIcon.vue";
import type { IconName } from "../components/GothamIcon.vue";
import MeCard from "../components/MeCard.vue";
import ServerRail from "../components/ServerRail.vue";
import { useAuthStore } from "../stores/auth";
import { useServersStore } from "../stores/servers";

/**
 * Listener ports shown as topbar chips. They mirror the backend defaults in
 * `internal/config/config.go` (`defaultServerPort = 8000`,
 * `defaultGRPCAddr = ":9442"`); the SPA itself is served from the same
 * origin, so these are labels, not live config.
 */
const cpPort = 8000;
const grpcPort = 9442;

/** Sidebar entry: live route when `to` is set, inert stub otherwise. */
interface NavItem {
  key: string;
  label: string;
  icon: IconName;
  to?: string;
}

interface NavSection {
  label: string;
  items: NavItem[];
}

// Group order and English labels follow the docs/design shell renderer
// (docs/design/assets/gotham-ui.js SECTIONS). Entries with no route are inert
// and say so — every backend phase has shipped, so no phase number is claimed.
const navSections: NavSection[] = [
  {
    label: "Operations",
    items: [
      { key: "dashboard", label: "Dashboard", icon: "grid", to: "dashboard" },
      { key: "applications", label: "Applications", icon: "box", to: "applications" },
      { key: "services", label: "Services", icon: "layers", to: "services" },
      { key: "databases", label: "Databases", icon: "db", to: "databases" },
      { key: "files", label: "File manager", icon: "folder" },
      { key: "templates", label: "Template library", icon: "rocket", to: "templates" },
      { key: "servers", label: "Servers", icon: "server", to: "servers" },
      { key: "domains", label: "Domains & SSL", icon: "globe", to: "domains" },
    ],
  },
  {
    label: "Team",
    items: [
      { key: "teams", label: "Members & roles", icon: "users", to: "teams" },
      {
        key: "notifications",
        label: "Notification channels",
        icon: "bell",
        to: "notifications",
      },
      { key: "tokens", label: "API tokens", icon: "key" },
    ],
  },
  {
    label: "System",
    items: [
      { key: "updates", label: "Updates & settings", icon: "gear" },
    ],
  },
];

const authStore = useAuthStore();
const serversStore = useServersStore();
const route = useRoute();
const router = useRouter();

const accountOptions: DropdownOption[] = [{ label: "Sign out", key: "sign-out" }];

// Live count: the only pill backed by a store. Every other section has no
// backend yet, so no pill is rendered rather than a fabricated number.
const serversCount = computed<number>(() => serversStore.servers.length);

// Section aliases for paths whose first segment is not the sidebar key.
const sectionAliases: Record<string, string> = { settings: "notifications" };

/**
 * activeKey is the sidebar entry for the current route. It follows the first
 * path segment, so detail routes (/servers/:id, /applications/:id,
 * /databases/:id, /services/:id) keep their section highlighted (B2-12, B3-5).
 */
const activeKey = computed<string>(() => {
  const segment = route.path.split("/").filter(Boolean)[0] ?? "dashboard";
  return sectionAliases[segment] ?? segment;
});

const userInitial = computed<string>(() =>
  (authStore.user?.email?.[0] ?? "?").toUpperCase(),
);

// The control plane exposes no environment endpoint, so the chip reflects
// where the SPA itself is served from: loopback means a local setup.
const envLabel = computed<string>(() => {
  const host = window.location.hostname;
  return host === "localhost" || host === "127.0.0.1" || host === "::1" || host === ""
    ? "local"
    : "production";
});

async function handleAccountSelect(key: string | number): Promise<void> {
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

const mobileNavOpen = ref(false);
const sidebarRef = ref<HTMLElement | null>(null);

// mobileQuery tracks the same ≤1024px breakpoint the stylesheet uses.
const mobileQuery = window.matchMedia("(max-width: 1024px)");

/** restoreFocus is the element focus returns to when the drawer closes. */
let restoreFocus: HTMLElement | null = null;

/** focusables lists the visible tab stops inside the drawer. */
function focusables(root: HTMLElement): HTMLElement[] {
  const selector =
    'a[href], button:not([disabled]), input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])';
  return Array.from(root.querySelectorAll<HTMLElement>(selector)).filter(
    (element) => element.offsetParent !== null,
  );
}

/** onDrawerKeydown closes on Escape and traps Tab inside the open drawer. */
function onDrawerKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    event.preventDefault();
    closeMobileNav();
    return;
  }
  if (event.key !== "Tab") {
    return;
  }
  const root = sidebarRef.value;
  if (!root) {
    return;
  }
  const items = focusables(root);
  if (items.length === 0) {
    return;
  }
  const first = items[0];
  const last = items[items.length - 1];
  const active = document.activeElement;
  if (event.shiftKey && (active === first || !root.contains(active))) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && active === last) {
    event.preventDefault();
    first.focus();
  }
}

/** toggleNav opens or closes the mobile drawer (the toggle is mobile-only). */
function toggleNav(): void {
  if (mobileNavOpen.value) {
    closeMobileNav();
    return;
  }
  restoreFocus =
    document.activeElement instanceof HTMLElement ? document.activeElement : null;
  mobileNavOpen.value = true;
}

/** closeMobileNav closes the drawer; the watcher below restores focus. */
function closeMobileNav(): void {
  mobileNavOpen.value = false;
}

watch(mobileNavOpen, async (open) => {
  if (open) {
    window.addEventListener("keydown", onDrawerKeydown);
    await nextTick();
    const root = sidebarRef.value;
    if (root) {
      (focusables(root)[0] ?? root).focus();
    }
    return;
  }
  window.removeEventListener("keydown", onDrawerKeydown);
  restoreFocus?.focus();
  restoreFocus = null;
});

// B3-17: growing past the breakpoint must unmount the drawer and its backdrop.
function onViewportChange(event: MediaQueryListEvent): void {
  if (!event.matches) {
    closeMobileNav();
  }
}

onMounted(() => mobileQuery.addEventListener("change", onViewportChange));

onBeforeUnmount(() => {
  mobileQuery.removeEventListener("change", onViewportChange);
  window.removeEventListener("keydown", onDrawerKeydown);
});

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
    :class="{ 'is-open': mobileNavOpen }"
  >
    <ServerRail />

    <aside
      id="app-nav"
      ref="sidebarRef"
      class="sidebar"
      aria-label="Product navigation"
      tabindex="-1"
    >
      <div class="sidebar-head">
        <span class="brand">Gotham</span>
        <span class="tag" :title="`Web build ${appVersion}`">v{{ appVersion }}</span>
      </div>
      <nav class="sidebar-body">
        <template v-for="section in navSections" :key="section.label">
          <p class="nav-label">{{ section.label }}</p>
          <template v-for="item in section.items" :key="item.key">
            <RouterLink
              v-if="item.to"
              class="nav-item"
              :class="{ 'is-active': activeKey === item.key }"
              :to="{ name: item.to }"
            >
              <GothamIcon :name="item.icon" />
              <span>{{ item.label }}</span>
              <span v-if="item.key === 'servers'" class="nav-count">{{ serversCount }}</span>
            </RouterLink>
            <NTooltip
              v-else
              trigger="hover"
              :tooltip-style="{ maxWidth: '240px' }"
            >
              <template #trigger>
                <button
                  type="button"
                  class="nav-item is-disabled"
                  aria-disabled="true"
                  :aria-label="`${item.label} — no UI yet`"
                  @click.prevent
                >
                  <GothamIcon :name="item.icon" />
                  <span>{{ item.label }}</span>
                </button>
              </template>
              {{ item.label }} — no UI yet
            </NTooltip>
          </template>
        </template>
      </nav>
      <div class="sidebar-foot">
        <MeCard />
      </div>
    </aside>

    <div class="main">
      <header class="topbar">
        <NButton
          quaternary
          circle
          class="nav-toggle"
          aria-label="Toggle navigation"
          aria-controls="app-nav"
          :aria-expanded="mobileNavOpen"
          @click="toggleNav"
        >
          <template #icon>
            <GothamIcon name="grid" />
          </template>
        </NButton>
        <span class="status-line" :title="`Serving environment: ${envLabel}`">
          <GothamIcon name="shield" class="status-icon" />
          {{ envLabel }}
        </span>
        <span class="channel-chip" :title="`Control-plane HTTP port`">CP :{{ cpPort }}</span>
        <span class="channel-chip" :title="`Agent gRPC port`">gRPC :{{ grpcPort }}</span>
        <NTooltip trigger="hover">
          <template #trigger>
            <div class="search" role="search" aria-label="Search (coming soon)">
              <GothamIcon name="search" />
              <NInput disabled placeholder="Search apps, servers, databases…" aria-label="Search" />
              <span class="kbd">⌘K</span>
            </div>
          </template>
          Search is coming soon
        </NTooltip>
        <div class="topbar-right">
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton quaternary circle aria-label="Notifications (coming soon)" class="is-stub">
                <template #icon>
                  <GothamIcon name="bell" />
                </template>
              </NButton>
            </template>
            Notifications — no UI yet
          </NTooltip>
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton quaternary circle aria-label="Docs (coming soon)" class="is-stub">
                <template #icon>
                  <GothamIcon name="doc" />
                </template>
              </NButton>
            </template>
            Docs — coming soon
          </NTooltip>
          <NDropdown
            v-if="authStore.isAuthenticated"
            trigger="click"
            :options="accountOptions"
            @select="handleAccountSelect"
          >
            <NButton quaternary circle aria-label="Account">
              <template #icon>
                <NAvatar round :size="24" :src="authStore.user?.avatar">
                  {{ userInitial }}
                </NAvatar>
              </template>
            </NButton>
          </NDropdown>
          <RouterLink v-else to="/login">
            <NButton quaternary type="primary">Sign in</NButton>
          </RouterLink>
        </div>
      </header>

      <main class="view">
        <div class="page">
          <RouterView />
        </div>
      </main>
    </div>

    <div
      v-if="mobileNavOpen"
      class="backdrop"
      aria-hidden="true"
      @click="closeMobileNav"
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

.nav-item.is-disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

/* Inert entries are real disabled buttons (not spans with role=link), so they
   are announced as disabled controls; reset the UA button chrome. */
button.nav-item {
  border: 0;
  background: none;
  font-family: inherit;
  text-align: left;
  cursor: not-allowed;
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

.status-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
  color: var(--muted);
  white-space: nowrap;
}

.status-icon {
  width: 15px;
  height: 15px;
  flex: 0 0 auto;
}

.channel-chip {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 3px 6px;
  white-space: nowrap;
}

.search {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  background: var(--surface-warm);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
  height: 28px;
  width: min(360px, 38vw);
  color: var(--muted);
  margin-left: auto;
  cursor: not-allowed;
}

.search :deep(svg) {
  width: 15px;
  height: 15px;
  flex: 0 0 auto;
}

.search :deep(.n-input) {
  background: none;
  cursor: not-allowed;
}

.search :deep(.n-input .n-input__input-el) {
  cursor: not-allowed;
}

.kbd {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-bottom-width: 2px;
  border-radius: 3px;
  padding: 1px 5px;
  flex: 0 0 auto;
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: var(--space-1);
}

.is-stub {
  opacity: 0.65;
  cursor: default;
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
  .app {
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

@media (max-width: 860px) {
  .topbar .channel-chip,
  .topbar .status-line {
    display: none;
  }

  .search {
    width: auto;
  }

  .topbar .search :deep(.n-input) {
    width: 0;
    padding: 0;
  }
}

@media (max-width: 640px) {
  .app {
    --rail-w: 60px;
  }

  /* Phone: every nav entry is a 44px touch target (B3-13). */
  .nav-item {
    min-height: 44px;
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
