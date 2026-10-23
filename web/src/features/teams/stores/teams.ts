import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createTeam as createTeamRequest,
  deleteTeam as deleteTeamRequest,
  describeTeamError,
  isFeatureDisabled,
  listTeams,
  renameTeam as renameTeamRequest,
} from "@/features/teams/api/teams";
import type { Team } from "@/features/teams/api/teams";
import { activeTeamStorageKey } from "@/shared/api/token";

export const useTeamsStore = defineStore("teams", () => {
  const teams = ref<Team[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const errorRaw = ref<unknown>(null);
  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeTeamError(errorRaw.value),
  );
  const featureDisabled = ref(false);
  const activeTeamId = ref<string>(readStoredTeamId());

  const activeTeam = computed<Team | null>(
    () => teams.value.find((team) => team.id === activeTeamId.value) ?? null,
  );

  function roleOf(teamId: string): Team["role"] | null {
    return teams.value.find((team) => team.id === teamId)?.role ?? null;
  }

  function selectTeam(teamId: string): void {
    activeTeamId.value = teamId;
    persistTeamId(teamId);
  }

  function applyTeams(next: Team[]): void {
    teams.value = next;
    const stillMember = next.some((team) => team.id === activeTeamId.value);
    if (stillMember) {
      return;
    }
    const fallback = next.find((team) => team.is_personal) ?? next[0];
    selectTeam(fallback?.id ?? "");
  }

  async function fetchTeams(): Promise<void> {
    loading.value = true;
    errorRaw.value = null;
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
      errorRaw.value = err;
      throw err;
    } finally {
      loading.value = false;
    }
  }

  async function ensureTeams(): Promise<void> {
    if (loaded.value || loading.value) {
      return;
    }
    await fetchTeams().catch(() => undefined);
  }

  function reset(): void {
    teams.value = [];
    loading.value = false;
    loaded.value = false;
    errorRaw.value = null;
    featureDisabled.value = false;
    activeTeamId.value = "";
    persistTeamId("");
  }

  async function create(name: string): Promise<Team> {
    const team = await createTeamRequest(name);
    applyTeams([
      { ...team, role: "owner" },
      ...teams.value,
    ]);
    selectTeam(team.id);
    return team;
  }

  async function rename(id: string, name: string): Promise<Team> {
    const team = await renameTeamRequest(id, name);
    applyTeams(
      teams.value.map((item) => (item.id === id ? { ...item, ...team } : item)),
    );
    return team;
  }

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

function readStoredTeamId(): string {
  try {
    return localStorage.getItem(activeTeamStorageKey) ?? "";
  } catch {
    return "";
  }
}

function persistTeamId(teamId: string): void {
  try {
    if (teamId) {
      localStorage.setItem(activeTeamStorageKey, teamId);
    } else {
      localStorage.removeItem(activeTeamStorageKey);
    }
  } catch {
  }
}
