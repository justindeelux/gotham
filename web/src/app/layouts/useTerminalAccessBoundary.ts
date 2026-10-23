import { watch } from "vue";

import { useTeamsStore } from "@/features/teams";
import { useTerminalStore } from "@/features/terminal";

export function useTerminalAccessBoundary(): void {
  const teamsStore = useTeamsStore();
  const terminal = useTerminalStore();

  watch(
    () => teamsStore.activeTeamId,
    (next, previous) => {
      if (previous && next !== previous) {
        terminal.closeContainerSessions();
      }
    },
  );

  watch(
    () => teamsStore.activeTeam?.role ?? null,
    (role, previous) => {
      if (role === "read_only" && previous !== "read_only") {
        terminal.closeContainerSessions();
      }
    },
  );
}
