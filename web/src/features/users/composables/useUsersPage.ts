import { useMessage } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

import { useProjectsStore } from "@/features/projects";
import {
  describeUserError,
  inviteLink,
  inviteUser,
  listInvites,
  listUsers,
  removeUser,
  resetUser,
  revokeInvite,
  setUserProjects,
} from "@/features/users/api/users";
import type { Account, Invite } from "@/features/users/schemas/users";
import { isInviteEmailValid } from "@/features/users/schemas/users";
import { i18n } from "@/shared/i18n";

export interface UsersPageState {
  users: Ref<Account[]>;
  invites: Ref<Invite[]>;
  projects: ComputedRef<Array<{ label: string; value: string }>>;
  loading: Ref<boolean>;
  error: Ref<string | null>;
  inviteOpen: Ref<boolean>;
  inviteEmail: Ref<string>;
  inviteProjects: Ref<string[]>;
  inviteBusy: Ref<boolean>;
  inviteError: Ref<string | null>;
  inviteLinkValue: Ref<string>;
  accessTarget: Ref<Account | null>;
  accessProjects: Ref<string[]>;
  accessBusy: Ref<boolean>;
  accessError: Ref<string | null>;
  refresh: () => Promise<void>;
  openInvite: () => void;
  closeInvite: () => void;
  submitInvite: () => Promise<void>;
  copyInviteLink: () => Promise<void>;
  openAccess: (_account: Account) => void;
  closeAccess: () => void;
  submitAccess: () => Promise<void>;
  remove: (_account: Account) => Promise<void>;
  reset: (_account: Account) => Promise<void>;
  revoke: (_invite: Invite) => Promise<void>;
}

export function useUsersPage(): UsersPageState {
  const message = useMessage();
  const projectsStore = useProjectsStore();

  const users = ref<Account[]>([]);
  const invites = ref<Invite[]>([]);
  const loading = ref(false);
  const failure = ref<unknown>(null);
  const error = computed<string | null>(() =>
    failure.value === null ? null : describeUserError(failure.value),
  );
  const projects = computed<Array<{ label: string; value: string }>>(() =>
    projectsStore.projects.map((project) => ({
      label: project.name,
      value: project.id,
    })),
  );

  const inviteOpen = ref(false);
  const inviteEmail = ref("");
  const inviteProjects = ref<string[]>([]);
  const inviteBusy = ref(false);
  const inviteFailure = ref<unknown>(null);
  const inviteError = computed<string | null>(() =>
    inviteFailure.value === null ? null : describeUserError(inviteFailure.value),
  );
  const inviteLinkValue = ref("");

  const accessTarget = ref<Account | null>(null);
  const accessProjects = ref<string[]>([]);
  const accessBusy = ref(false);
  const accessFailure = ref<unknown>(null);
  const accessError = computed<string | null>(() =>
    accessFailure.value === null ? null : describeUserError(accessFailure.value),
  );

  async function refresh(): Promise<void> {
    loading.value = true;
    failure.value = null;
    try {
      const [nextUsers, nextInvites] = await Promise.all([
        listUsers(),
        listInvites(),
      ]);
      users.value = nextUsers;
      invites.value = nextInvites;
      if (!projectsStore.loaded) {
        await projectsStore.fetchProjects().catch(() => undefined);
      }
    } catch (err) {
      failure.value = err;
    } finally {
      loading.value = false;
    }
  }

  function openInvite(): void {
    inviteEmail.value = "";
    inviteProjects.value = [];
    inviteFailure.value = null;
    inviteLinkValue.value = "";
    inviteOpen.value = true;
  }

  function closeInvite(): void {
    inviteOpen.value = false;
  }

  async function submitInvite(): Promise<void> {
    if (inviteBusy.value || !isInviteEmailValid(inviteEmail.value)) {
      return;
    }
    inviteBusy.value = true;
    inviteFailure.value = null;
    try {
      const created = await inviteUser(inviteEmail.value, inviteProjects.value);
      inviteLinkValue.value = inviteLink(created.token);
      message.success(String(messageText("users.toast.invited")));
      await refresh();
    } catch (err) {
      inviteFailure.value = err;
    } finally {
      inviteBusy.value = false;
    }
  }

  async function copyInviteLink(): Promise<void> {
    if (inviteLinkValue.value === "") {
      return;
    }
    try {
      await navigator.clipboard.writeText(inviteLinkValue.value);
      message.success(String(messageText("users.toast.linkCopied")));
    } catch {
      message.warning(String(messageText("users.toast.clipboardDenied")));
    }
  }

  function openAccess(account: Account): void {
    accessTarget.value = account;
    accessProjects.value = account.projects.map((project) => project.id);
    accessFailure.value = null;
  }

  function closeAccess(): void {
    accessTarget.value = null;
  }

  async function submitAccess(): Promise<void> {
    const target = accessTarget.value;
    if (!target || accessBusy.value) {
      return;
    }
    accessBusy.value = true;
    accessFailure.value = null;
    try {
      const updated = await setUserProjects(target.id, accessProjects.value);
      users.value = users.value.map((account) =>
        account.id === updated.id ? updated : account,
      );
      message.success(String(messageText("users.toast.accessSaved")));
      accessTarget.value = null;
    } catch (err) {
      accessFailure.value = err;
    } finally {
      accessBusy.value = false;
    }
  }

  async function remove(account: Account): Promise<void> {
    try {
      await removeUser(account.id);
      users.value = users.value.filter((item) => item.id !== account.id);
      message.success(
        String(messageText("users.toast.removed", { email: account.email })),
      );
    } catch (err) {
      message.error(describeUserError(err));
    }
  }

  async function reset(account: Account): Promise<void> {
    try {
      await resetUser(account.id);
      message.success(
        String(messageText("users.toast.reset", { email: account.email })),
      );
    } catch (err) {
      message.error(describeUserError(err));
    }
  }

  async function revoke(invite: Invite): Promise<void> {
    try {
      await revokeInvite(invite.id);
      invites.value = invites.value.filter((item) => item.id !== invite.id);
      message.success(
        String(messageText("users.toast.inviteRevoked", { email: invite.email })),
      );
    } catch (err) {
      message.error(describeUserError(err));
    }
  }

  onMounted(() => {
    void refresh();
  });

  return {
    users,
    invites,
    projects,
    loading,
    error,
    inviteOpen,
    inviteEmail,
    inviteProjects,
    inviteBusy,
    inviteError,
    inviteLinkValue,
    accessTarget,
    accessProjects,
    accessBusy,
    accessError,
    refresh,
    openInvite,
    closeInvite,
    submitInvite,
    copyInviteLink,
    openAccess,
    closeAccess,
    submitAccess,
    remove,
    reset,
    revoke,
  };
}

function messageText(key: string, params?: Record<string, string>): string {
  return String(i18n.global.t(key, params ?? {}));
}
