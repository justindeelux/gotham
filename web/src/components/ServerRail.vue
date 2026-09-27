<script setup lang="ts">
import { NTooltip } from "naive-ui";
import { computed, onMounted, onUnmounted, watch } from "vue";
import { RouterLink, useRoute } from "vue-router";

import type { Server, ServerStatus } from "../api/servers";
import { useServersStore } from "../stores/servers";

const serversStore = useServersStore();
const route = useRoute();

const isDashboard = computed<boolean>(() => route.name === "dashboard");

const alertCount = computed<number>(
  () =>
    serversStore.servers.filter(
      (server) => server.status === "offline" || server.status === "error",
    ).length,
);

const alertsLabel = computed<string>(() =>
  alertCount.value === 0
    ? "No new alerts"
    : `${alertCount.value} new alert${alertCount.value === 1 ? "" : "s"}`,
);

const statusDots: Record<ServerStatus, string> = {
  ready: "dot--online",
  validating: "dot--idle",
  pending: "dot--idle",
  offline: "dot--offline",
  error: "dot--dnd",
};

const statusLabels: Record<ServerStatus, string> = {
  ready: "Ready",
  validating: "Validating",
  pending: "Pending",
  offline: "Offline",
  error: "Error",
};

/** serverInitials derives a two-letter avatar from the server name. */
function serverInitials(name: string): string {
  const parts = name.split(/[^A-Za-z0-9]+/).filter((part) => part.length > 0);
  if (parts.length === 0) {
    return "?";
  }
  if (parts.length === 1) {
    return parts[0].slice(0, 2).toUpperCase();
  }
  return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
}

/** serverTip builds the English tooltip for one rail avatar. */
function serverTip(server: Server): string {
  return `${server.name} · ${statusLabels[server.status] ?? server.status}`;
}

onMounted(() => {
  if (serversStore.servers.length === 0) {
    void serversStore.fetchServers().catch(() => {
      // Read-only rail: the servers page surfaces load errors.
    });
  }
  serversStore.pollServers();
});

// A page may stop the shared poll timer on unmount; re-arm it so the rail
// keeps showing live status on every route.
watch(
  () => route.path,
  () => {
    serversStore.pollServers();
  },
);

onUnmounted(() => {
  serversStore.stopPolling();
});
</script>

<template>
  <nav class="rail" aria-label="Servers">
    <div class="rail-item" :class="{ 'is-active': isDashboard }">
      <NTooltip placement="right" trigger="hover">
        <template #trigger>
          <RouterLink
            class="rail-btn"
            :to="{ name: 'dashboard' }"
            aria-label="Gotham — home"
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              aria-hidden="true"
            >
              <path d="M5 19V11a7 7 0 0114 0v8" />
              <path d="M9.5 19v-6.5a2.5 2.5 0 015 0V19" />
            </svg>
          </RouterLink>
        </template>
        <span>Gotham — home</span>
      </NTooltip>
    </div>

    <div class="rail-sep" aria-hidden="true"></div>

    <div class="rail-nav" role="list" aria-label="Managed servers">
      <div
        v-for="server in serversStore.servers"
        :key="server.id"
        class="rail-item"
        role="listitem"
      >
        <NTooltip placement="right" trigger="hover">
          <template #trigger>
            <RouterLink
              class="rail-btn"
              :to="{ name: 'servers' }"
              :aria-label="serverTip(server)"
            >
              {{ serverInitials(server.name) }}
              <span
                class="rail-dot"
                :class="statusDots[server.status]"
                aria-hidden="true"
              ></span>
            </RouterLink>
          </template>
          <span>{{ serverTip(server) }}</span>
        </NTooltip>
      </div>
    </div>

    <div class="rail-item">
      <NTooltip placement="right" trigger="hover">
        <template #trigger>
          <RouterLink
            class="rail-btn rail-btn--add"
            :to="{ name: 'servers' }"
            aria-label="Add server"
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linecap="round"
              stroke-linejoin="round"
              aria-hidden="true"
            >
              <path d="M12 5v14M5 12h14" />
            </svg>
          </RouterLink>
        </template>
        <span>Add server</span>
      </NTooltip>
    </div>

    <div class="rail-foot">
      <div class="rail-sep" aria-hidden="true"></div>
      <div class="rail-item">
        <NTooltip placement="right" trigger="hover">
          <template #trigger>
            <RouterLink
              class="rail-btn"
              :to="{ name: 'dashboard' }"
              aria-label="Overview"
            >
              <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
              >
                <path d="M4 7h10M18 7h2M4 17h4M12 17h8" />
                <circle cx="16" cy="7" r="2" />
                <circle cx="10" cy="17" r="2" />
              </svg>
            </RouterLink>
          </template>
          <span>Overview</span>
        </NTooltip>
      </div>
      <div class="rail-item">
        <NTooltip placement="right" trigger="hover">
          <template #trigger>
            <RouterLink
              class="rail-btn"
              :to="{ name: 'servers' }"
              :aria-label="`System alerts — ${alertsLabel}`"
            >
              <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
              >
                <path
                  d="M6 9a6 6 0 1112 0c0 5 2 6 2 6H4s2-1 2-6zM10 20a2 2 0 004 0"
                />
              </svg>
              <span
                v-if="alertCount > 0"
                class="rail-badge"
                aria-hidden="true"
                >{{ alertCount > 9 ? "9+" : alertCount }}</span
              >
            </RouterLink>
          </template>
          <span>{{ alertsLabel }}</span>
        </NTooltip>
      </div>
    </div>
  </nav>
</template>

<style scoped>
.rail {
  background: var(--surface-warm);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: var(--space-3) 0;
  gap: var(--space-2);
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
}

.rail-nav {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
}

.rail-sep {
  width: 32px;
  height: 2px;
  border-radius: var(--radius-pill);
  background: var(--border-soft);
  margin: var(--space-1) 0;
  flex: 0 0 auto;
}

.rail-item {
  position: relative;
  display: grid;
  place-items: center;
  width: 100%;
}

.rail-btn {
  position: relative;
  width: 48px;
  height: 48px;
  display: grid;
  place-items: center;
  background: var(--bg);
  color: var(--fg-2);
  font-family: var(--font-display);
  font-size: var(--text-lg);
  font-weight: 700;
  border-radius: var(--radius-lg);
  transition:
    border-radius 350ms var(--ease-morph),
    background var(--motion-base) var(--ease-standard),
    color var(--motion-base) var(--ease-standard);
}

.rail-btn:hover,
.rail-btn:focus-visible {
  border-radius: var(--radius-pill);
  background: var(--accent);
  color: var(--accent-on);
}

.rail-btn svg {
  width: 22px;
  height: 22px;
}

.rail-btn--add {
  background: var(--surface);
  color: var(--success);
}

.rail-item.is-active .rail-btn {
  border-radius: var(--radius-lg);
  background: var(--accent);
  color: var(--accent-on);
}

.rail-item.is-active .rail-btn:hover {
  border-radius: var(--radius-pill);
}

.rail-item::before {
  content: "";
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 4px;
  height: 0;
  background: var(--fg-2);
  border-radius: 0 var(--radius-pill) var(--radius-pill) 0;
  transition: height var(--motion-base) var(--ease-standard);
}

.rail-item:hover::before {
  height: 20px;
}

.rail-item.is-active::before {
  height: 40px;
}

.rail-dot {
  position: absolute;
  right: 10px;
  bottom: 9px;
  width: 10px;
  height: 10px;
  border-radius: var(--radius-pill);
  border: 3px solid var(--surface-warm);
  box-sizing: content-box;
}

.dot--online {
  background: var(--success);
}

.dot--idle {
  background: var(--warn);
}

.dot--dnd {
  background: var(--danger);
}

.dot--offline {
  background: var(--meta);
}

.rail-badge {
  position: absolute;
  right: 6px;
  top: 4px;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  display: grid;
  place-items: center;
  background: var(--danger);
  color: var(--accent-on);
  border: 3px solid var(--surface-warm);
  box-sizing: content-box;
  border-radius: var(--radius-pill);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
  line-height: 1;
}

.rail-foot {
  margin-top: auto;
  position: sticky;
  bottom: 0;
  z-index: 2;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding-top: var(--space-2);
  background: var(--surface-warm);
}
</style>
