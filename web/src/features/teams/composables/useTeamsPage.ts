import { useMessage } from "naive-ui";
import { computed, inject, onMounted, ref, watch } from "vue";
import type { ComputedRef, InjectionKey, Ref } from "vue";

import type { CreatedTeamInvite, Team, TeamInvite, TeamMember, TeamRole } from "@/features/teams/api/teams";
import {
  canManageMembers,
  createInvite,
  describeTeamError,
  listInvites,
  listMembers,
  removeMember,
  revokeInvite,
  roleLabel,
  teamText,
  updateMemberRole,
} from "@/features/teams/api/teams";
import { useTeamsStore } from "@/features/teams/stores/teams";
import { registerLink } from "@/features/teams/utils/teamLinks";

export interface TeamsPageContext {
  selectedTeam: ComputedRef<Team | null>;
  isPersonal: ComputedRef<boolean>;
  canManage: ComputedRef<boolean>;
  isOwner: ComputedRef<boolean>;
  personalOwnerId: ComputedRef<string>;
  roleOptions: ComputedRef<Array<{ label: string; value: TeamRole }>>;
  inviteRoleOptions: ComputedRef<Array<{ label: string; value: TeamRole }>>;
  createOpen: Ref<boolean>;
  createName: Ref<string>;
  createBusy: Ref<boolean>;
  createError: Ref<string | null>;
  renameOpen: Ref<boolean>;
  renameName: Ref<string>;
  renameBusy: Ref<boolean>;
  renameError: Ref<string | null>;
  teamActionError: Ref<string | null>;
  deleting: Ref<boolean>;
  members: Ref<TeamMember[]>;
  membersLoading: Ref<boolean>;
  membersError: Ref<string | null>;
  memberActionError: Ref<string | null>;
  invites: Ref<TeamInvite[]>;
  invitesLoading: Ref<boolean>;
  invitesError: Ref<string | null>;
  inviteActionError: Ref<string | null>;
  inviteOpen: Ref<boolean>;
  inviteEmail: Ref<string>;
  inviteRole: Ref<TeamRole>;
  inviteBusy: Ref<boolean>;
  inviteError: Ref<string | null>;
  createdInvite: Ref<CreatedTeamInvite | null>;
  copied: Ref<boolean>;
  memberRoleDisabled(_member: TeamMember): boolean;
  memberRemovable(_member: TeamMember): boolean;
  memberPending(_userId: string): boolean;
  loadTeam(): Promise<void>;
  handleCreate(): Promise<void>;
  openRename(): void;
  handleRename(): Promise<void>;
  handleDelete(): Promise<void>;
  changeMemberRole(_member: TeamMember, _role: TeamRole): Promise<void>;
  handleRemoveMember(_member: TeamMember): Promise<void>;
  openInvite(): void;
  handleCreateInvite(): Promise<void>;
  handleRevokeInvite(_invite: TeamInvite): Promise<void>;
  copyAcceptLink(): Promise<void>;
  closeInviteToken(): void;
}

export const teamsPageKey: InjectionKey<TeamsPageContext> = Symbol("teams-page");

export function useTeamsPageContext(): TeamsPageContext {
  const context = inject(teamsPageKey);
  if (!context) {
    throw new Error("useTeamsPageContext must be used inside TeamsPage.");
  }
  return context;
}

export function useTeamsPage(): TeamsPageContext {
  const message = useMessage();
  const teamsStore = useTeamsStore();

  const createOpen = ref(false);
  const createName = ref("");
  const createBusy = ref(false);
  const createErrorRaw = ref<unknown>(null);
  const createError = computed<string | null>(() =>
    createErrorRaw.value === null
      ? null
      : describeTeamError(createErrorRaw.value),
  );

  const renameOpen = ref(false);
  const renameName = ref("");
  const renameBusy = ref(false);
  const renameErrorRaw = ref<unknown>(null);
  const renameError = computed<string | null>(() =>
    renameErrorRaw.value === null
      ? null
      : describeTeamError(renameErrorRaw.value),
  );

  const teamActionErrorRaw = ref<unknown>(null);
  const teamActionError = computed<string | null>(() =>
    teamActionErrorRaw.value === null
      ? null
      : describeTeamError(teamActionErrorRaw.value),
  );
  const deleting = ref(false);

  const members = ref<TeamMember[]>([]);
  const membersLoading = ref(false);
  const membersErrorRaw = ref<unknown>(null);
  const membersError = computed<string | null>(() =>
    membersErrorRaw.value === null
      ? null
      : describeTeamError(membersErrorRaw.value),
  );
  const memberActionErrorRaw = ref<unknown>(null);
  const memberActionError = computed<string | null>(() =>
    memberActionErrorRaw.value === null
      ? null
      : describeTeamError(memberActionErrorRaw.value),
  );
  let membersReadToken = 0;
  const pendingMemberIds = ref<Record<string, boolean>>({});

  const invites = ref<TeamInvite[]>([]);
  const invitesLoading = ref(false);
  const invitesErrorRaw = ref<unknown>(null);
  const invitesError = computed<string | null>(() =>
    invitesErrorRaw.value === null
      ? null
      : describeTeamError(invitesErrorRaw.value),
  );
  const inviteActionErrorRaw = ref<unknown>(null);
  const inviteActionError = computed<string | null>(() =>
    inviteActionErrorRaw.value === null
      ? null
      : describeTeamError(inviteActionErrorRaw.value),
  );
  let invitesReadToken = 0;

  let selectionGeneration = 0;

  const mutationTokens = new Map<string, number>();

  let mutationSequence = 0;

  function beginMutation(subject: string): number {
    mutationSequence += 1;
    mutationTokens.set(subject, mutationSequence);
    return mutationSequence;
  }

  function ownsMutation(
    subject: string,
    token: number,
    teamId: string,
    generation: number,
  ): boolean {
    return (
      generation === selectionGeneration &&
      teamsStore.activeTeamId === teamId &&
      mutationTokens.get(subject) === token
    );
  }

  function setMemberPending(userId: string, pending: boolean): void {
    const next = { ...pendingMemberIds.value };
    if (pending) {
      next[userId] = true;
    } else {
      delete next[userId];
    }
    pendingMemberIds.value = next;
  }

  function memberPending(userId: string): boolean {
    return pendingMemberIds.value[userId] === true;
  }

  const inviteOpen = ref(false);
  const inviteEmail = ref("");
  const inviteRole = ref<TeamRole>("read_only");
  const inviteBusy = ref(false);
  const inviteErrorRaw = ref<unknown>(null);
  const inviteError = computed<string | null>(() =>
    inviteErrorRaw.value === null
      ? null
      : describeTeamError(inviteErrorRaw.value),
  );
  const createdInvite = ref<CreatedTeamInvite | null>(null);
  const copied = ref(false);

  const selectedTeam = computed(() => teamsStore.activeTeam);
  const isPersonal = computed<boolean>(() => selectedTeam.value?.is_personal ?? false);
  const canManage = computed<boolean>(() => canManageMembers(selectedTeam.value?.role ?? null));
  const isOwner = computed<boolean>(() => selectedTeam.value?.role === "owner");
  const personalOwnerId = computed<string>(() =>
    isPersonal.value ? selectedTeam.value?.id ?? "" : "",
  );

  const roleOptions = computed<Array<{ label: string; value: TeamRole }>>(
    () => [
      { label: roleLabel("owner"), value: "owner" },
      { label: roleLabel("admin"), value: "admin" },
      { label: roleLabel("read_only"), value: "read_only" },
    ],
  );

  const inviteRoleOptions = computed<Array<{ label: string; value: TeamRole }>>(
    () => [
      { label: roleLabel("admin"), value: "admin" },
      { label: roleLabel("read_only"), value: "read_only" },
    ],
  );

  function memberRoleDisabled(member: TeamMember): boolean {
    if (!canManage.value) {
      return true;
    }
    if (member.user_id === personalOwnerId.value) {
      return true;
    }
    return member.role === "owner" && !isOwner.value;
  }

  function memberRemovable(member: TeamMember): boolean {
    if (!canManage.value) {
      return false;
    }
    return member.user_id !== personalOwnerId.value;
  }

  function resetTeamContext(): void {
    selectionGeneration += 1;
    membersReadToken += 1;
    invitesReadToken += 1;
    mutationTokens.clear();
    members.value = [];
    invites.value = [];
    membersErrorRaw.value = null;
    invitesErrorRaw.value = null;
    memberActionErrorRaw.value = null;
    inviteActionErrorRaw.value = null;
    membersLoading.value = false;
    invitesLoading.value = false;
    pendingMemberIds.value = {};
    deleting.value = false;
    teamActionErrorRaw.value = null;
    inviteBusy.value = false;
    inviteOpen.value = false;
  }

  async function loadMembers(): Promise<void> {
    const teamId = teamsStore.activeTeamId;
    const token = ++membersReadToken;
    const isCurrent = (): boolean =>
      token === membersReadToken && teamsStore.activeTeamId === teamId;
    membersErrorRaw.value = null;
    memberActionErrorRaw.value = null;
    if (!teamId) {
      return;
    }
    membersLoading.value = true;
    try {
      const loaded = await listMembers(teamId);
      if (!isCurrent()) {
        return;
      }
      members.value = loaded;
    } catch (error) {
      if (!isCurrent()) {
        return;
      }
      membersErrorRaw.value = error;
    } finally {
      if (isCurrent()) {
        membersLoading.value = false;
      }
    }
  }

  async function loadInvites(): Promise<void> {
    const teamId = teamsStore.activeTeamId;
    const token = ++invitesReadToken;
    const isCurrent = (): boolean =>
      token === invitesReadToken && teamsStore.activeTeamId === teamId;
    invitesErrorRaw.value = null;
    inviteActionErrorRaw.value = null;
    if (!teamId) {
      return;
    }
    invitesLoading.value = true;
    try {
      const loaded = await listInvites(teamId);
      if (!isCurrent()) {
        return;
      }
      invites.value = loaded;
    } catch (error) {
      if (!isCurrent()) {
        return;
      }
      invitesErrorRaw.value = error;
    } finally {
      if (isCurrent()) {
        invitesLoading.value = false;
      }
    }
  }

  async function loadTeam(): Promise<void> {
    await Promise.all([loadMembers(), loadInvites()]);
  }

  async function handleCreate(): Promise<void> {
    createBusy.value = true;
    createErrorRaw.value = null;
    try {
      const team = await teamsStore.create(createName.value.trim());
      message.success(teamText("teams.toast.createdTeam", "Created team {name}", { name: team.name }));
      createOpen.value = false;
      createName.value = "";
    } catch (error) {
      createErrorRaw.value = error;
    } finally {
      createBusy.value = false;
    }
  }

  function openRename(): void {
    renameName.value = selectedTeam.value?.name ?? "";
    renameErrorRaw.value = null;
    renameOpen.value = true;
  }

  async function handleRename(): Promise<void> {
    const team = selectedTeam.value;
    if (!team) {
      return;
    }
    renameBusy.value = true;
    renameErrorRaw.value = null;
    try {
      await teamsStore.rename(team.id, renameName.value.trim());
      message.success(teamText("teams.toast.renamed", "Team renamed"));
      renameOpen.value = false;
    } catch (error) {
      renameErrorRaw.value = error;
    } finally {
      renameBusy.value = false;
    }
  }

  async function handleDelete(): Promise<void> {
    const team = selectedTeam.value;
    if (!team) {
      return;
    }
    const generation = selectionGeneration;
    const teamId = team.id;
    const subject = `team:${teamId}`;
    const token = beginMutation(subject);
    const teamGone = (): boolean =>
      !teamsStore.teams.some((item) => item.id === teamId);
    const ownsFeedback = (): boolean =>
      mutationTokens.get(subject) === token &&
      generation === selectionGeneration;
    deleting.value = true;
    teamActionErrorRaw.value = null;
    try {
      await teamsStore.remove(teamId);
      if (teamGone()) {
        message.success(teamText("teams.toast.deletedTeam", "Deleted team {name}", { name: team.name }));
      }
    } catch (error) {
      if (!ownsFeedback()) {
        return;
      }
      teamActionErrorRaw.value = error;
    } finally {
      if (ownsFeedback()) {
        deleting.value = false;
      }
    }
  }

  async function changeMemberRole(member: TeamMember, role: TeamRole): Promise<void> {
    const teamId = teamsStore.activeTeamId;
    if (!teamId || role === member.role) {
      return;
    }
    const generation = selectionGeneration;
    const token = beginMutation(member.user_id);
    const owns = (): boolean => ownsMutation(member.user_id, token, teamId, generation);
    setMemberPending(member.user_id, true);
    memberActionErrorRaw.value = null;
    try {
      const updated = await updateMemberRole(teamId, member.user_id, role);
      if (!owns()) {
        return;
      }
      members.value = members.value.map((item) =>
        item.user_id === updated.user_id ? updated : item,
      );
      message.success(teamText("teams.toast.roleChanged", "{email} is now {role}", { email: member.email, role: roleLabel(role) }));
    } catch (error) {
      if (!owns()) {
        return;
      }
      memberActionErrorRaw.value = error;
    } finally {
      if (owns()) {
        setMemberPending(member.user_id, false);
      }
    }
  }

  async function handleRemoveMember(member: TeamMember): Promise<void> {
    const teamId = teamsStore.activeTeamId;
    if (!teamId) {
      return;
    }
    const generation = selectionGeneration;
    const token = beginMutation(member.user_id);
    const owns = (): boolean => ownsMutation(member.user_id, token, teamId, generation);
    setMemberPending(member.user_id, true);
    memberActionErrorRaw.value = null;
    try {
      await removeMember(teamId, member.user_id);
      if (!owns()) {
        return;
      }
      members.value = members.value.filter((item) => item.user_id !== member.user_id);
      message.success(teamText("teams.toast.removedMember", "Removed {email}", { email: member.email }));
    } catch (error) {
      if (!owns()) {
        return;
      }
      memberActionErrorRaw.value = error;
    } finally {
      if (owns()) {
        setMemberPending(member.user_id, false);
      }
    }
  }

  function openInvite(): void {
    inviteEmail.value = "";
    inviteRole.value = "read_only";
    inviteErrorRaw.value = null;
    inviteOpen.value = true;
  }

  async function handleCreateInvite(): Promise<void> {
    const team = selectedTeam.value;
    if (!team) {
      return;
    }
    const generation = selectionGeneration;
    const token = beginMutation("invite:create");
    const owns = (): boolean =>
      ownsMutation("invite:create", token, team.id, generation);
    inviteBusy.value = true;
    inviteErrorRaw.value = null;
    try {
      const invite = await createInvite(
        team.id,
        inviteEmail.value.trim(),
        inviteRole.value,
      );
      if (!owns()) {
        return;
      }
      createdInvite.value = invite;
      copied.value = false;
      inviteOpen.value = false;
      await loadInvites();
    } catch (error) {
      if (!owns()) {
        return;
      }
      inviteErrorRaw.value = error;
    } finally {
      if (owns()) {
        inviteBusy.value = false;
      }
    }
  }

  async function handleRevokeInvite(invite: TeamInvite): Promise<void> {
    const teamId = teamsStore.activeTeamId;
    if (!teamId) {
      return;
    }
    const generation = selectionGeneration;
    const subject = `invite:${invite.id}`;
    const token = beginMutation(subject);
    const owns = (): boolean => ownsMutation(subject, token, teamId, generation);
    inviteActionErrorRaw.value = null;
    try {
      await revokeInvite(teamId, invite.id);
      if (!owns()) {
        return;
      }
      invites.value = invites.value.filter((item) => item.id !== invite.id);
      message.success(teamText("teams.toast.revokedInvite", "Revoked the invite to {email}", { email: invite.email }));
    } catch (error) {
      if (!owns()) {
        return;
      }
      inviteActionErrorRaw.value = error;
    }
  }

  async function copyAcceptLink(): Promise<void> {
    const invite = createdInvite.value;
    if (!invite) {
      return;
    }
    try {
      await navigator.clipboard.writeText(registerLink(invite.token));
      copied.value = true;
      message.success(teamText("teams.toast.linkCopied", "Invite link copied"));
    } catch {
      message.warning(teamText("teams.toast.clipboardDenied", "Clipboard is unavailable — select the link and copy it manually."));
    }
  }

  function closeInviteToken(): void {
    createdInvite.value = null;
    copied.value = false;
  }

  watch(
    () => teamsStore.activeTeamId,
    () => {
      resetTeamContext();
      closeInviteToken();
      teamActionErrorRaw.value = null;
      void loadTeam();
    },
  );

  onMounted(async () => {
    const selectionBeforeLoad = teamsStore.activeTeamId;
    try {
      await teamsStore.fetchTeams();
    } catch {
    }
    if (teamsStore.activeTeamId === selectionBeforeLoad) {
      await loadTeam();
    }
  });

  return {
    selectedTeam,
    isPersonal,
    canManage,
    isOwner,
    personalOwnerId,
    roleOptions,
    inviteRoleOptions,
    createOpen,
    createName,
    createBusy,
    createError,
    renameOpen,
    renameName,
    renameBusy,
    renameError,
    teamActionError,
    deleting,
    members,
    membersLoading,
    membersError,
    memberActionError,
    invites,
    invitesLoading,
    invitesError,
    inviteActionError,
    inviteOpen,
    inviteEmail,
    inviteRole,
    inviteBusy,
    inviteError,
    createdInvite,
    copied,
    memberRoleDisabled,
    memberRemovable,
    memberPending,
    loadTeam,
    handleCreate,
    openRename,
    handleRename,
    handleDelete,
    changeMemberRole,
    handleRemoveMember,
    openInvite,
    handleCreateInvite,
    handleRevokeInvite,
    copyAcceptLink,
    closeInviteToken,
  };
}
