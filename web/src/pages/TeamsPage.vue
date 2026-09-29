<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NInput,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted, ref, watch } from "vue";

import type { CreatedTeamInvite, TeamInvite, TeamMember, TeamRole } from "../api/teams";
import {
  canManageMembers,
  createInvite,
  describeTeamError,
  listInvites,
  listMembers,
  removeMember,
  revokeInvite,
  roleLabel,
  roleTagType,
  updateMemberRole,
} from "../api/teams";
import { useTeamsStore } from "../stores/teams";
import { expiryLabel, relativeTime } from "../utils/format";

/**
 * FE-8.1 (2) — teams, members and invites.
 *
 * Ported from docs/design/team-settings.html (members + invites panels) onto
 * the established settings page pattern. Every mutation is gated on the
 * caller's role as `GET /v1/teams` reports it, and the backend's protections
 * (last owner, personal team, non-empty team) surface as inline errors
 * instead of being pre-empted: the service is the authority.
 */
const message = useMessage();
const teamsStore = useTeamsStore();

// Create / rename / delete.
const createOpen = ref(false);
const createName = ref("");
const createBusy = ref(false);
const createError = ref<string | null>(null);

const renameOpen = ref(false);
const renameName = ref("");
const renameBusy = ref(false);
const renameError = ref<string | null>(null);

const teamActionError = ref<string | null>(null);
const deleting = ref(false);

// Members.
const members = ref<TeamMember[]>([]);
const membersLoading = ref(false);
const membersError = ref<string | null>(null);
const memberActionError = ref<string | null>(null);
/** Token of the newest members read; a stale response never writes state. */
let membersReadToken = 0;
/** Members with an in-flight mutation, keyed by user id, for the row spinner. */
const pendingMemberIds = ref<Record<string, boolean>>({});

// Invites.
const invites = ref<TeamInvite[]>([]);
const invitesLoading = ref(false);
const invitesError = ref<string | null>(null);
const inviteActionError = ref<string | null>(null);
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
const inviteError = ref<string | null>(null);
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

const roleOptions: Array<{ label: string; value: TeamRole }> = [
  { label: "owner", value: "owner" },
  { label: "admin", value: "admin" },
  { label: "read-only", value: "read_only" },
];

const inviteRoleOptions: Array<{ label: string; value: TeamRole }> = [
  { label: "admin", value: "admin" },
  { label: "read-only", value: "read_only" },
];

/** acceptLink builds the one-time accept URL for an invite token. */
function acceptLink(token: string): string {
  return `${window.location.origin}/invite/accept?token=${encodeURIComponent(token)}`;
}

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

const memberColumns = computed<DataTableColumns<TeamMember>>(() => [
  {
    title: "Member",
    key: "email",
    minWidth: 220,
    render: (row) =>
      h("div", { style: "display:flex;flex-direction:column;gap:2px" }, [
        h("span", { class: "mono", style: "color:var(--fg-2)" }, row.email),
        row.user_id === personalOwnerId.value
          ? h(
              "span",
              { class: "muted", style: "font-size:var(--text-xs)" },
              "personal team owner",
            )
          : null,
      ]),
  },
  {
    title: "Role",
    key: "role",
    width: 150,
    render: (row) => {
      if (memberRoleDisabled(row)) {
        return h(
          NTag,
          { type: roleTagType(row.role), size: "small", round: true },
          { default: () => roleLabel(row.role) },
        );
      }
      return h(NSelect, {
        value: row.role,
        size: "small",
        options: roleOptions,
        "aria-label": `Role of ${row.email}`,
        loading: memberPending(row.user_id),
        "onUpdate:value": (role: TeamRole) => void changeMemberRole(row, role),
      });
    },
  },
  {
    title: "Joined",
    key: "created_at",
    width: 130,
    render: (row) => h("span", { class: "mono" }, relativeTime(row.created_at)),
  },
  {
    title: "Actions",
    key: "actions",
    width: 110,
    render: (row) =>
      memberRemovable(row)
        ? h(
            NPopconfirm,
            {
              positiveButtonProps: { type: "error" },
              onPositiveClick: () => void handleRemoveMember(row),
            },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: "small",
                    ghost: true,
                    type: "error",
                    loading: memberPending(row.user_id),
                  },
                  { default: () => "Remove" },
                ),
              default: () => `Remove ${row.email} from ${selectedTeam.value?.name ?? "the team"}?`,
            },
          )
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
]);

const inviteColumns = computed<DataTableColumns<TeamInvite>>(() => [
  {
    title: "Email",
    key: "email",
    minWidth: 220,
    render: (row) => h("span", { class: "mono fg-2" }, row.email),
  },
  {
    title: "Role",
    key: "role",
    width: 120,
    render: (row) =>
      h(NTag, { size: "small", round: true }, { default: () => roleLabel(row.role) }),
  },
  {
    title: "Sent",
    key: "created_at",
    width: 130,
    render: (row) => h("span", { class: "mono" }, relativeTime(row.created_at)),
  },
  {
    title: "Expires",
    key: "expires_at",
    width: 150,
    render: (row) => h("span", { class: "mono" }, expiryLabel(row.expires_at)),
  },
  {
    title: "Actions",
    key: "actions",
    width: 120,
    render: (row) =>
      h(
        NPopconfirm,
        {
          positiveButtonProps: { type: "error" },
          onPositiveClick: () => void handleRevokeInvite(row),
        },
        {
          trigger: () =>
            h(NButton, { size: "small", ghost: true, type: "error" }, { default: () => "Revoke" }),
          default: () => `Revoke the invite to ${row.email}?`,
        },
      ),
  },
]);

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
  membersError.value = null;
  invitesError.value = null;
  memberActionError.value = null;
  inviteActionError.value = null;
  membersLoading.value = false;
  invitesLoading.value = false;
  pendingMemberIds.value = {};
  deleting.value = false;
  teamActionError.value = null;
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
  membersError.value = null;
  memberActionError.value = null;
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
    membersError.value = describeTeamError(error);
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
  invitesError.value = null;
  inviteActionError.value = null;
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
    invitesError.value = describeTeamError(error);
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
  createError.value = null;
  try {
    const team = await teamsStore.create(createName.value.trim());
    message.success(`Created team ${team.name}`);
    createOpen.value = false;
    createName.value = "";
    // Selecting the new team fires the selection watcher, which reloads the
    // collections; an explicit load here would duplicate that read.
  } catch (error) {
    createError.value = describeTeamError(error);
  } finally {
    createBusy.value = false;
  }
}

/** openRename prefills the rename dialog with the selected team's name. */
function openRename(): void {
  renameName.value = selectedTeam.value?.name ?? "";
  renameError.value = null;
  renameOpen.value = true;
}

async function handleRename(): Promise<void> {
  const team = selectedTeam.value;
  if (!team) {
    return;
  }
  renameBusy.value = true;
  renameError.value = null;
  try {
    await teamsStore.rename(team.id, renameName.value.trim());
    message.success("Team renamed");
    renameOpen.value = false;
  } catch (error) {
    renameError.value = describeTeamError(error);
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
  teamActionError.value = null;
  try {
    await teamsStore.remove(teamId);
    if (teamGone()) {
      message.success(`Deleted team ${team.name}`);
    }
    // The store falls back to the personal team, whose selection change
    // reloads the collections through the watcher.
  } catch (error) {
    // The backend message is the actionable part: personal teams and teams
    // that still own resources are refused with 409.
    if (!ownsFeedback()) {
      return;
    }
    teamActionError.value = describeTeamError(error);
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
  memberActionError.value = null;
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
    message.success(`${member.email} is now ${roleLabel(role)}`);
  } catch (error) {
    if (!owns()) {
      return;
    }
    memberActionError.value = describeTeamError(error);
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
  memberActionError.value = null;
  try {
    await removeMember(teamId, member.user_id);
    if (!owns()) {
      return;
    }
    members.value = members.value.filter((item) => item.user_id !== member.user_id);
    message.success(`Removed ${member.email}`);
  } catch (error) {
    if (!owns()) {
      return;
    }
    memberActionError.value = describeTeamError(error);
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
  inviteError.value = null;
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
  inviteError.value = null;
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
    inviteError.value = describeTeamError(error);
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
  inviteActionError.value = null;
  try {
    await revokeInvite(teamId, invite.id);
    if (!owns()) {
      return;
    }
    invites.value = invites.value.filter((item) => item.id !== invite.id);
    message.success(`Revoked the invite to ${invite.email}`);
  } catch (error) {
    if (!owns()) {
      return;
    }
    inviteActionError.value = describeTeamError(error);
  }
}

/** copyAcceptLink copies the one-time link; a denied clipboard falls back. */
async function copyAcceptLink(): Promise<void> {
  const invite = createdInvite.value;
  if (!invite) {
    return;
  }
  try {
    await navigator.clipboard.writeText(acceptLink(invite.token));
    copied.value = true;
    message.success("Invite link copied");
  } catch {
    message.warning("Clipboard is unavailable — select the link and copy it manually.");
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
    teamActionError.value = null;
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
</script>

<template>
  <div class="teams-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Team · Access control</p>
        <h1>Teams</h1>
        <p class="page-desc">
          Every resource belongs to exactly one team, and a member holds one of
          three roles — <span class="mono">owner</span> /
          <span class="mono">admin</span> /
          <span class="mono">read-only</span>. Owners manage ownership,
          admins manage resources and members, read-only members may only look.
        </p>
      </div>
      <div class="page-actions">
        <NButton
          v-if="!teamsStore.featureDisabled"
          type="primary"
          @click="createOpen = true"
        >
          New team
        </NButton>
      </div>
    </div>

    <NCard v-if="teamsStore.featureDisabled" title="Teams unavailable">
      <NEmpty
        description="Team management is not enabled on this control plane (FEATURE_TEAMS=false)."
      />
    </NCard>

    <template v-else>
      <NAlert v-if="teamsStore.error" type="error" :show-icon="true">
        {{ teamsStore.error }}
      </NAlert>

      <NCard title="Your teams">
        <template #header-extra>
          <NText depth="3">{{ teamsStore.teams.length }} team(s)</NText>
        </template>
        <NAlert
          v-if="teamActionError"
          type="error"
          :show-icon="true"
          style="margin-bottom: 12px"
          data-testid="team-action-error"
        >
          {{ teamActionError }}
        </NAlert>
        <NSpin :show="teamsStore.loading">
          <div
            v-for="team in teamsStore.teams"
            :key="team.id"
            class="team-row"
            :class="{ 'is-selected': team.id === teamsStore.activeTeamId }"
            :data-team="team.name"
          >
            <NButton
              quaternary
              size="small"
              class="team-row__select"
              :aria-pressed="team.id === teamsStore.activeTeamId"
              @click="teamsStore.selectTeam(team.id)"
            >
              {{ team.name }}
            </NButton>
            <NTag v-if="team.is_personal" size="small" round>personal</NTag>
            <NTag :type="roleTagType(team.role)" size="small" round>
              {{ roleLabel(team.role) }}
            </NTag>
            <NText depth="3" class="mono team-row__id">{{ team.id.slice(0, 8) }}</NText>
            <NText depth="3" class="team-row__age">{{ relativeTime(team.created_at) }}</NText>
            <div class="team-row__actions">
              <NButton
                size="small"
                quaternary
                :disabled="team.id !== teamsStore.activeTeamId"
                @click="openRename"
              >
                Rename
              </NButton>
              <NPopconfirm
                v-if="team.role === 'owner'"
                :positive-button-props="{ type: 'error' }"
                @positive-click="handleDelete"
              >
                <template #trigger>
                  <NButton
                    size="small"
                    ghost
                    type="error"
                    :disabled="team.is_personal || team.id !== teamsStore.activeTeamId"
                    :loading="deleting && team.id === teamsStore.activeTeamId"
                  >
                    Delete
                  </NButton>
                </template>
                Delete team "{{ team.name }}"? Resources must be moved first —
                a team that still owns any is refused.
              </NPopconfirm>
            </div>
          </div>
          <NEmpty
            v-if="
              !teamsStore.loading &&
              teamsStore.teams.length === 0 &&
              !teamsStore.error &&
              teamsStore.loaded
            "
            description="No teams yet."
          />
        </NSpin>
      </NCard>

      <NCard v-if="selectedTeam" :title="selectedTeam.name">
        <template #header-extra>
          <NSpace align="center" :size="8">
            <NButton
              size="small"
              :loading="membersLoading || invitesLoading"
              @click="void loadTeam()"
            >
              Refresh
            </NButton>
            <NTag v-if="isPersonal" size="small" round>personal</NTag>
            <NTag :type="roleTagType(selectedTeam.role)" size="small" round>
              your role: {{ roleLabel(selectedTeam.role) }}
            </NTag>
          </NSpace>
        </template>

        <NTabs type="line" animated>
          <NTabPane name="members" :tab="`Members (${members.length})`">
            <NSpace vertical :size="12" style="margin-top: 12px">
              <NAlert
                v-if="membersError"
                type="error"
                :show-icon="true"
                data-testid="members-error"
              >
                {{ membersError }}
              </NAlert>
              <NAlert
                v-if="memberActionError"
                type="error"
                :show-icon="true"
                data-testid="member-action-error"
              >
                {{ memberActionError }}
              </NAlert>
              <NDataTable
                :columns="memberColumns"
                :data="members"
                :loading="membersLoading"
                :row-key="(row: TeamMember) => row.user_id"
                :bordered="false"
                :scroll-x="760"
                :pagination="false"
                data-testid="members-table"
              />
              <NText depth="3">
                A team always keeps at least one owner: demoting or removing the
                last one is refused with the backend's message. The personal
                team's owner membership is immutable.
              </NText>
            </NSpace>
          </NTabPane>

          <NTabPane name="invites" :tab="`Invites (${invites.length})`">
            <NSpace vertical :size="12" style="margin-top: 12px">
              <NAlert
                v-if="invitesError"
                type="error"
                :show-icon="true"
                data-testid="invites-error"
              >
                {{ invitesError }}
              </NAlert>
              <NAlert
                v-if="inviteActionError"
                type="error"
                :show-icon="true"
                data-testid="invite-action-error"
              >
                {{ inviteActionError }}
              </NAlert>
              <div v-if="canManage" class="invites-actions">
                <NButton size="small" type="primary" @click="openInvite">
                  Invite member
                </NButton>
                <NText depth="3">
                  Invites are single-use and bound to the email address.
                </NText>
              </div>
              <NDataTable
                :columns="inviteColumns"
                :data="invites"
                :loading="invitesLoading"
                :row-key="(row: TeamInvite) => row.id"
                :bordered="false"
                :scroll-x="760"
                :pagination="false"
                data-testid="invites-table"
              />
              <NText v-if="!canManage" depth="3">
                Your role does not list or manage invites.
              </NText>
            </NSpace>
          </NTabPane>
        </NTabs>
      </NCard>
    </template>

    <NModal
      v-if="!teamsStore.featureDisabled"
      v-model:show="createOpen"
      preset="card"
      title="New team"
      style="width: 460px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          You become the new team's owner. Resources can be moved in from the
          applications and servers you already manage.
        </NText>
        <NInput
          v-model:value="createName"
          placeholder="Team name"
          aria-label="Team name"
          @keyup.enter="void handleCreate()"
        />
        <NAlert v-if="createError" type="error" :show-icon="true">
          {{ createError }}
        </NAlert>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="createOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="createBusy"
            :disabled="createName.trim() === ''"
            @click="void handleCreate()"
          >
            Create team
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="renameOpen"
      preset="card"
      title="Rename team"
      style="width: 460px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NInput
          v-model:value="renameName"
          aria-label="Team name"
          @keyup.enter="void handleRename()"
        />
        <NAlert v-if="renameError" type="error" :show-icon="true">
          {{ renameError }}
        </NAlert>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="renameOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="renameBusy"
            :disabled="renameName.trim() === ''"
            @click="void handleRename()"
          >
            Save
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="inviteOpen"
      preset="card"
      title="Invite member"
      style="width: 480px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NInput
          v-model:value="inviteEmail"
          placeholder="name@example.com"
          aria-label="Invite email"
          @keyup.enter="void handleCreateInvite()"
        />
        <NSelect
          v-model:value="inviteRole"
          :options="inviteRoleOptions"
          aria-label="Invite role"
        />
        <NText depth="3">
          Invites can grant admin or read-only — ownership is transferred from
          the members table, never invited.
        </NText>
        <NAlert v-if="inviteError" type="error" :show-icon="true">
          {{ inviteError }}
        </NAlert>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="inviteOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="inviteBusy"
            :disabled="inviteEmail.trim() === ''"
            @click="void handleCreateInvite()"
          >
            Create invite
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      :show="createdInvite !== null"
      preset="card"
      title="Invite created — copy the link now"
      style="width: 560px; max-width: 94vw"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) closeInviteToken(); }"
    >
      <NSpace v-if="createdInvite" vertical :size="12">
        <NAlert type="warning" :show-icon="true">
          This link is shown once and never stored. Email delivery is not wired
          yet (BE-8.3) — send it to {{ createdInvite.email }} yourself.
        </NAlert>
        <NInput
          :value="acceptLink(createdInvite.token)"
          readonly
          class="mono"
          aria-label="Accept link"
          data-testid="invite-accept-link"
        />
        <NText depth="3" class="mono token-text" data-testid="invite-token">
          token: {{ createdInvite.token }}
        </NText>
        <NText depth="3">
          The recipient posts this token to
          <span class="mono">{{ createdInvite.accept_url }}</span> while signed
          in as {{ createdInvite.email }}. It expires
          {{ expiryLabel(createdInvite.expires_at) }}.
        </NText>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="void copyAcceptLink()">
            {{ copied ? "Copied" : "Copy link" }}
          </NButton>
          <NButton type="primary" @click="closeInviteToken()">Done</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.teams-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.team-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  border: 1px solid transparent;
}

.team-row.is-selected {
  background: var(--selected-row);
  border-color: var(--border);
}

.team-row__select {
  font-weight: 600;
}

.team-row__id {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
}

.team-row__age {
  font-size: var(--text-xs);
}

.team-row__actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.invites-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.token-text {
  word-break: break-all;
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
  }

  .team-row {
    flex-wrap: wrap;
  }
}
</style>
