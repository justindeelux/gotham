import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createTeam as createTeamRequest,
  deleteTeam as deleteTeamRequest,
  describeTeamError,
  isFeatureDisabled,
  listTeams,
  renameTeam as renameTeamRequest,
} from "../api/teams";
import type { Team } from "../api/teams";

/** localStorage key the active team selection is persisted under. */
export const activeTeamStorageKey = "gotham.teams.active";

/**
 * Teams: the caller's team list plus the active team selection.
 *
 * The active team is what the team-scoped surfaces (notification channels
 * today) send as `X-Team-Id`; when it is empty the control plane falls back to
 * the caller's personal team, which keeps every pre-teams surface unchanged.
 * The selection is persisted so it survives a reload, but it is validated
 * against the freshly read list on every load — a team the caller left can
 * never stay selected.
 *
 * `featureDisabled` is set when GET /teams answers 404 (FEATURE_TEAMS=false):
 * the pages then hide the team-management surface instead of rendering the
 * 404 as an error.
 */
export const useTeamsStore = defineStore("teams", () => {
  const teams = ref<Team[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const error = ref<string | null>(null);
  const featureDisabled = ref(false);
  const activeTeamId = ref<string>(readStoredTeamId());

  const activeTeam = computed<Team | null>(
    () => teams.value.find((team) => team.id === activeTeamId.value) ?? null,
  );

  /** roles reads the caller's role in a team, if it is known. */
  function roleOf(teamId: string): Team["role"] | null {
    return teams.value.find((team) => team.id === teamId)?.role ?? null;
  }

  /** selectTeam switches the active team and persists the selection. */
  function selectTeam(teamId: string): void {
    activeTeamId.value = teamId;
    persistTeamId(teamId);
  }

  /**
   * applyTeams replaces the list and re-validates the active selection: the
   * personal team wins, then a still-existing selection, then the first team.
   * An empty list clears the selection back to the personal-team fallback.
   */
  function applyTeams(next: Team[]): void {
    teams.value = next;
    const stillMember = next.some((team) => team.id === activeTeamId.value);
    if (stillMember) {
      return;
    }
    const fallback = next.find((team) => team.is_personal) ?? next[0];
    selectTeam(fallback?.id ?? "");
  }

  /** fetchTeams loads the caller's teams, newest first. */
  async function fetchTeams(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      const next = await listTeams();
      featureDisabled.value = false;
      loaded.value = true;
      applyTeams(next);
    } catch (err) {
      if (isFeatureDisabled(err)) {
        featureDisabled.value = true;
        loaded.value = true;
        applyTeams([]);
        return;
      }
      error.value = describeTeamError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** ensureTeams loads the list once; later callers reuse the cache. */
  async function ensureTeams(): Promise<void> {
    if (loaded.value || loading.value) {
      return;
    }
    await fetchTeams().catch(() => undefined);
  }

  /**
   * reset drops the cached teams and the persisted selection, so the next
   * sign-in starts from an empty cache instead of the previous account's
   * roles. Called on sign-out (see the auth store): without it the sidebar
   * role can belong to the previous user, because ensureTeams returns early
   * once `loaded` is set.
   */
  function reset(): void {
    teams.value = [];
    loading.value = false;
    loaded.value = false;
    error.value = null;
    featureDisabled.value = false;
    activeTeamId.value = "";
    persistTeamId("");
  }

  /** create stores a team and selects it. */
  async function create(name: string): Promise<Team> {
    const team = await createTeamRequest(name);
    applyTeams([
      { ...team, role: "owner" },
      ...teams.value,
    ]);
    selectTeam(team.id);
    return team;
  }

  /** rename patches one team's name in the cached list. */
  async function rename(id: string, name: string): Promise<Team> {
    const team = await renameTeamRequest(id, name);
    applyTeams(
      teams.value.map((item) => (item.id === id ? { ...item, ...team } : item)),
    );
    return team;
  }

  /** remove deletes a team and drops it from the cached list. */
  async function remove(id: string): Promise<void> {
    await deleteTeamRequest(id);
    applyTeams(teams.value.filter((item) => item.id !== id));
  }

  return {
    teams,
    loading,
    loaded,
    error,
    featureDisabled,
    activeTeamId,
    activeTeam,
    roleOf,
    selectTeam,
    fetchTeams,
    ensureTeams,
    reset,
    create,
    rename,
    remove,
  };
});

/** readStoredTeamId reads the persisted selection, tolerating a dead store. */
function readStoredTeamId(): string {
  try {
    return localStorage.getItem(activeTeamStorageKey) ?? "";
  } catch {
    return "";
  }
}

/** persistTeamId writes the selection, tolerating a dead store. */
function persistTeamId(teamId: string): void {
  try {
    if (teamId) {
      localStorage.setItem(activeTeamStorageKey, teamId);
    } else {
      localStorage.removeItem(activeTeamStorageKey);
    }
  } catch {
    // A disabled localStorage must never break team switching.
  }
}
