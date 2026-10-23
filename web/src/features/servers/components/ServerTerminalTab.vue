<script setup lang="ts">
import type { Server } from "@/features/servers";
import { useServerTerminalTab } from "@/features/servers/composables/useServerTerminalTab";
import { TerminalSessionStage } from "@/features/terminal";

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
