<script setup lang="ts">
import { computed, onMounted, onUnmounted } from "vue";
import { useRoute } from "vue-router";

import RailIconButton from "@/features/servers/components/RailIconButton.vue";
import {
  alertsLabel,
  countAlerts,
  serverInitials,
  serverTip,
  statusDots,
} from "@/features/servers/utils/serverRailView";
import { useServersStore } from "@/features/servers/stores/servers";

const serversStore = useServersStore();
const route = useRoute();

const isDashboard = computed<boolean>(() => route.name === "dashboard");

const alertCount = computed<number>(() => countAlerts(serversStore.servers));

const alertText = computed<string>(() => alertsLabel(alertCount.value));

// The persistent rail owns the shared server poll: it arms the interval on
// mount and is the only component that clears it (on its own unmount). Pages
// fetch on mount but never stop the timer, so navigation cannot freeze the rail
// dots (B4-7).
onMounted(() => {
  if (serversStore.servers.length === 0) {
    void serversStore.fetchServers().catch(() => {
      // Read-only rail: the servers page surfaces load errors.
    });
  }
  serversStore.pollServers();
});

onUnmounted(() => {
  serversStore.stopPolling();
});
</script>

<template>
  <nav class="rail" aria-label="Servers">
    <div class="rail-item" :class="{ 'is-active': isDashboard }">
      <RailIconButton :to="{ name: 'dashboard' }" label="Gotham — home">
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
      </RailIconButton>
    </div>

    <div class="rail-sep" aria-hidden="true"></div>

    <div class="rail-nav" role="list" aria-label="Managed servers">
      <div
        v-for="server in serversStore.servers"
        :key="server.id"
        class="rail-item"
        role="listitem"
      >
        <RailIconButton
          :to="{ name: 'server-detail', params: { id: server.id } }"
          :label="serverTip(server)"
        >
          {{ serverInitials(server.name) }}
          <span
            class="rail-dot"
            :class="statusDots[server.status]"
            aria-hidden="true"
          ></span>
        </RailIconButton>
      </div>
    </div>

    <div class="rail-item">
      <RailIconButton
        :to="{ name: 'servers', query: { add: '1' } }"
        label="Add server"
        link-class="rail-btn--add"
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
      </RailIconButton>
    </div>

    <div class="rail-foot">
      <div class="rail-sep" aria-hidden="true"></div>
      <div class="rail-item">
        <RailIconButton :to="{ name: 'dashboard' }" label="Overview">
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
        </RailIconButton>
      </div>
      <div class="rail-item">
        <RailIconButton
          :to="{ name: 'servers' }"
          :label="`System alerts — ${alertText}`"
          :tip="alertText"
          :badge-count="alertCount"
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
        </RailIconButton>
      </div>
    </div>
  </nav>
</template>

<style scoped>
/* :deep() on .rail-btn/.rail-badge: those elements render inside
 * RailIconButton, so the parent scope alone cannot reach them; the deep
 * selector keeps one rule covering both the icon buttons and the inline
 * server avatars with identical computed styles. */
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

:deep(.rail-btn) {
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

:deep(.rail-btn:hover),
:deep(.rail-btn:focus-visible) {
  border-radius: var(--radius-pill);
  background: var(--accent);
  color: var(--accent-on);
}

:deep(.rail-btn) svg {
  width: 22px;
  height: 22px;
}

:deep(.rail-btn--add) {
  background: var(--surface);
  color: var(--success);
}

.rail-item.is-active :deep(.rail-btn) {
  border-radius: var(--radius-lg);
  background: var(--accent);
  color: var(--accent-on);
}

.rail-item.is-active :deep(.rail-btn:hover) {
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
  right: 3px;
  bottom: 3px;
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
  background: var(--danger);
}

:deep(.rail-badge) {
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
