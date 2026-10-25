import { computed, ref, watch } from "vue";
import { useRouter } from "vue-router";

import { useAuthStore } from "@/features/auth";
import type { Server } from "@/features/servers";
import { useTerminalStore } from "@/features/terminal";

export interface ServerTerminalTabOptions {
  serverId: () => string;
  server: () => Server | null;
  ready: () => boolean;
  active: () => boolean;
  onBlocked: () => void;
}

export function useServerTerminalTab(options: ServerTerminalTabOptions) {
  const router = useRouter();
  const authStore = useAuthStore();
  const terminal = useTerminalStore();

  const tabId = ref<string>("");
  const starting = ref(false);
  const closed = ref(false);

  const canUse = computed<boolean>(
    () => authStore.isAuthenticated && authStore.isOwner,
  );
  const tab = computed(() =>
    tabId.value === "" ? null : terminal.findTab(tabId.value),
  );

  function sourcePath(id: string): string {
    return router.resolve({
      name: "server-detail",
      params: { id },
      query: { tab: "terminal" },
    }).fullPath;
  }

  function reset(): void {
    tabId.value = "";
    starting.value = false;
    closed.value = false;
  }

  async function open(fresh: boolean): Promise<void> {
    const server = options.server();
    if (
      !server ||
      server.id !== options.serverId() ||
      !options.ready() ||
      !canUse.value
    ) {
      return;
    }
    const request = {
      serverId: server.id,
      serverName: server.name,
      sourcePath: sourcePath(server.id),
    };
    if (!fresh) {
      const existing = terminal.findTabForTarget(request);
      if (existing) {
        tabId.value = existing.id;
        closed.value = false;
        return;
      }
    }
    closed.value = false;
    starting.value = true;
    try {
      const session = await terminal.openPageSession(
        fresh ? { ...request, fresh: true } : request,
      );
      if (options.serverId() === server.id) {
        tabId.value = session.id;
      }
    } finally {
      starting.value = false;
    }
  }

  function startNew(): void {
    void open(true);
  }

  watch(tab, (current) => {
    if (tabId.value !== "" && current === null) {
      tabId.value = "";
      starting.value = false;
      closed.value = true;
    }
  });

  watch(
    [
      () => options.active(),
      () => options.server()?.status,
      () => options.server()?.id,
      canUse,
    ],
    () => {
      if (
        !options.active() ||
        closed.value ||
        tabId.value !== "" ||
        !options.ready() ||
        !canUse.value
      ) {
        return;
      }
      void open(false);
    },
    { immediate: true },
  );

  watch(canUse, (allowed) => {
    if (!allowed && authStore.isAuthenticated) {
      reset();
      options.onBlocked();
    }
  });

  watch(() => options.serverId(), reset);

  return { tabId, starting, closed, startNew };
}
