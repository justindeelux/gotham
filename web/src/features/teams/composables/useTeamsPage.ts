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

/**
 * Shared page context for the teams surface (FE-8.1 (2) — teams, members and
 * invites). Created once by TeamsPage and consumed by its panels and dialogs
 * through `useTeamsPageContext`, so no prop drilling is needed.
 */
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

/** useTeamsPageContext reads the page context provided by TeamsPage. */
export function useTeamsPageContext(): TeamsPageContext {
  const context = inject(teamsPageKey);
  if (!context) {
    throw new Error("useTeamsPageContext must be used inside TeamsPage.");
  }
  return context;
}

/**
 * useTeamsPage owns every piece of teams page state: team create / rename /
 * delete, the members and invites collections with their guarded loads and
 * mutations, and the invite dialogs. See the original TeamsPage for the
 * ownership protocol (selection generations, mutation tokens, read tokens).
 */
export function useTeamsPage(): TeamsPageContext {
  const message = useMessage();
  const teamsStore = useTeamsStore();

  // Create / rename / delete.
  const createOpen = ref(false);
  const createName = ref("");
  const createBusy = ref(false);
  /**
   * Raw failures behind the dialog and page alerts. Display strings derive
   * from these plus the current locale, so a language switch refreshes a
   * retained alert without losing typed input.
   */
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

  // Members.
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
  /** Token of the newest members read; a stale response never writes state. */
  let membersReadToken = 0;
  /** Members with an in-flight mutation, keyed by user id, for the row spinner. */
  const pendingMemberIds = ref<Record<string, boolean>>({});

  // Invites.
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
  /** Token of the newest invites read; a stale response never writes state. */
  let invitesReadToken = 0;

  // Mutation ownership.
  /**
   * Selection generation: incremented whenever the selected team changes. A
   * mutation captures it at start and may only write page state while it still
   * matches, so a stale response from an earlier visit to the same team
   * (A → B → A) can never overwrite the newer visit's applied result.
   */
  let selectionGeneration = 0;

  /**
   * Latest mutation token per subject (a member id, an invite id, the invite
   * form, the team delete). Only the newest mutation of one subject may write
   * page state, so an overlapping older submission cannot regress to its
   * response or clear the newer one's pending indicator. The counter itself
   * never resets, so a token minted after a selection change can never collide
   * with one that is still in flight.
   */
  const mutationTokens = new Map<string, number>();

  /** Monotonic mutation counter; every issuance is unique. */
  let mutationSequence = 0;

  /** beginMutation registers a mutation of one subject and returns its token. */
  function beginMutation(subject: string): number {
    mutationSequence += 1;
    mutationTokens.set(subject, mutationSequence);
    return mutationSequence;
  }

  /**
   * ownsMutation reports whether a mutation may still write page state: the
   * selection must not have changed since it started, it must still target the
   * selected team, and no newer mutation of the same subject may have started.
   */
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

  /** setMemberPending tracks one member's in-flight mutation. */
  function setMemberPending(userId: string, pending: boolean): void {
    const next = { ...pendingMemberIds.value };
    if (pending) {
      next[userId] = true;
    } else {
      delete next[userId];
    }
    pendingMemberIds.value = next;
  }

  /** memberPending reports whether a member row awaits its mutation. */
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
  /** The one-time create answer; it lives in memory only and is never stored. */
  const createdInvite = ref<CreatedTeamInvite | null>(null);
  const copied = ref(false);

  const selectedTeam = computed(() => teamsStore.activeTeam);
  const isPersonal = computed<boolean>(() => selectedTeam.value?.is_personal ?? false);
  const canManage = computed<boolean>(() => canManageMembers(selectedTeam.value?.role ?? null));
  const isOwner = computed<boolean>(() => selectedTeam.value?.role === "owner");
  /** The personal team's owner membership is immutable (see ErrPersonalOwner). */
  const personalOwnerId = computed<string>(() =>
    isPersonal.value ? selectedTeam.value?.id ?? "" : "",
  );

  /**
   * Role select options in the current locale. roleLabel reads the shared
   * role catalog at invocation time, so the computed refreshes on a
   * language switch. Values are wire roles and never translated.
   */
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

  /** memberRoleDisabled reports whether a member's role select is locked. */
  function memberRoleDisabled(member: TeamMember): boolean {
    if (!canManage.value) {
      return true;
    }
    if (member.user_id === personalOwnerId.value) {
      return true;
    }
    // Only an owner may touch an owner (or grant ownership).
    return member.role === "owner" && !isOwner.value;
  }

  /** memberRemovable reports whether the remove action is offered. */
  function memberRemovable(member: TeamMember): boolean {
    if (!canManage.value) {
      return false;
    }
    return member.user_id !== personalOwnerId.value;
  }

  /**
   * resetTeamContext drops the previous team's members and invites, invalidates
   * every read and mutation still in flight for it, and clears its pending
   * indicators. It runs before a selection change is acted on, so rows, errors,
   * spinners and late responses of one team can never surface under another.
   */
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
    // The invite form belongs to the previous team: its pending submission is
    // now owned by nobody (the guard refuses to clear it), so reset the busy
    // state here or the next team's form would stay loading forever, and close
    // a form the new selection did not open.
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
      // Only the newest read owns the spinner; an obsolete one must not clear a
      // loading state the current read still needs.
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

  /** loadTeam refreshes the selected team's members and invites. */
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
      // Selecting the new team fires the selection watcher, which reloads the
      // collections; an explicit load here would duplicate that read.
    } catch (error) {
      createErrorRaw.value = error;
    } finally {
      createBusy.value = false;
    }
  }

  /** openRename prefills the rename dialog with the selected team's name. */
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
    /**
     * A team absent from the store is the genuine outcome of a successful delete
     * (the store removes the row and moves the selection), so that is what the
     * success message reports. Every other write — the failure alert and the
     * pending spinner — must own the current context (mutation token and
     * selection generation). The team-gone shortcut must never feed the error
     * path: once a newer delete has succeeded, an older failure would otherwise
     * paint its error on the fallback team.
     */
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
      // The store falls back to the personal team, whose selection change
      // reloads the collections through the watcher.
    } catch (error) {
      // The backend message is the actionable part: personal teams and teams
      // that still own resources are refused with 409.
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
        // A newer visit or a newer submission owns this row now: the response
        // and its success message belong to an earlier state.
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

  /** openInvite resets the invite dialog. */
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
        // The operator moved on, or a newer submission owns the form; the
        // one-time token belongs to the team the invite was created for and
        // must not surface under another one.
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

  /** copyAcceptLink copies the one-time link; a denied clipboard falls back. */
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

  /** closeInviteToken drops the one-time token from memory. */
  function closeInviteToken(): void {
    createdInvite.value = null;
    copied.value = false;
  }

  watch(
    () => teamsStore.activeTeamId,
    () => {
      // Drop and invalidate the previous team's collections before the new
      // reads start: no late response may render under the new selection.
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
      // The store exposes the error; the alert renders it.
    }
    // The selection watcher already started the reads when fetchTeams changed
    // the active team; only an unchanged selection needs an explicit first load.
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
