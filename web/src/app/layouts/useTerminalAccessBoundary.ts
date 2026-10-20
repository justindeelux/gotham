import { watch } from "vue";

import { useTeamsStore } from "@/features/teams";
import { useTerminalStore } from "@/features/terminal";

/**
 * The ONE owner of container-tab disposal at a permission boundary (JUS-247).
 *
 * A container shell belongs to the team that owns the application. Switching
 * the active team or losing the write (owner/admin) role disposes those tabs
 * instead of leaving an unauthorized shell reachable from the dock or the
 * rail; the backend also refuses the next revalidation. Host shells are
 * untouched (operator capability), and logout uses the auth flow's store
 * reset. The application page and tab host never close sessions themselves, so
 * a transient application refetch gap cannot kill a live shell.
 */
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
