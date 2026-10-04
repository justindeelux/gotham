<script setup lang="ts">
import { NAlert, NButton, NDataTable, NPopconfirm, NSpace, NTag, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h } from "vue";

import type { TeamInvite } from "@/features/teams/api/teams";
import { roleLabel } from "@/features/teams/api/teams";
import { expiryLabel, relativeTime } from "@/shared/utils/format";
import { useTeamsPageContext } from "@/features/teams/composables/useTeamsPage";

/** TeamInvitesPanel renders the invites tab: invite action, table, revoke. */
const {
  canManage,
  invites,
  invitesLoading,
  invitesError,
  inviteActionError,
  openInvite,
  handleRevokeInvite,
} = useTeamsPageContext();

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
</script>

<template>
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
</template>

<style scoped>
.invites-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}
</style>
