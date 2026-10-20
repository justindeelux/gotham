<script setup lang="ts">
import type { Server } from "@/features/servers";
import { useServerTerminalTab } from "@/features/servers/composables/useServerTerminalTab";
import { TerminalSessionStage } from "@/features/terminal";

/**
 * Server detail Terminal tab (JUS-246).
 *
 * The tab is a thin host: `useServerTerminalTab` owns the lazy start, the
 * closed/restart state and the demotion reset, while TerminalSessionStage is
 * the same state view the standalone page uses. Keeping the state here (and
 * the pane mounted with `display-directive="show"`) means switching tabs
 * after a close keeps the restart card and never starts a shell.
 */
interface Props {
  server: Server | null;
  serverId: string;
  ready: boolean;
  active: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{ blocked: [] }>();

const { tabId, starting, closed, startNew } = useServerTerminalTab({
  serverId: () => props.serverId,
  server: () => props.server,
  ready: () => props.ready,
  active: () => props.active,
  onBlocked: () => emit("blocked"),
});
</script>

<template>
  <TerminalSessionStage
    :server="server"
    :ready="ready"
    :tab-id="tabId"
    :starting="starting"
    :closed="closed"
    @start="startNew"
  />
</template>
