<script setup lang="ts">
import { NAlert, NButton, NDataTable, NPopconfirm, NSelect, NSpace, NTag, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h } from "vue";

import type { TeamMember, TeamRole } from "@/features/teams/api/teams";
import { roleLabel, roleTagType } from "@/features/teams/api/teams";
import { relativeTime } from "@/shared/utils/format";
import { useTeamsPageContext } from "@/features/teams/composables/useTeamsPage";

/** TeamMembersPanel renders the members tab: table, role selects, removal. */
const {
  selectedTeam,
  personalOwnerId,
  roleOptions,
  memberRoleDisabled,
  memberRemovable,
  memberPending,
  members,
  membersLoading,
  membersError,
  memberActionError,
  changeMemberRole,
  handleRemoveMember,
} = useTeamsPageContext();

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
</script>

<template>
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
</template>
