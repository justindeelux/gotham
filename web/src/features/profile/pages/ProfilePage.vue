<script setup lang="ts">
import { NSpace } from "naive-ui";

import ChangePasswordForm from "@/features/profile/components/ChangePasswordForm.vue";
import DisplayNameForm from "@/features/profile/components/DisplayNameForm.vue";
import ProfileIdentityCard from "@/features/profile/components/ProfileIdentityCard.vue";
import SessionsPanel from "@/features/profile/components/SessionsPanel.vue";

/**
 * Profile page (JUS-27, sessions JUS-28) — account facts, display name,
 * change password, active sessions.
 * Thin route component: each panel owns its state per mount.
 */
</script>

<template>
  <div class="profile-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Settings · Account</p>
        <h1>Profile</h1>
        <p class="page-desc">
          Your account facts, the name shown in the sidebar, and your
          password. Your email address cannot be changed here.
        </p>
      </div>
    </div>

    <NSpace vertical :size="16" class="profile-panels">
      <ProfileIdentityCard />
      <DisplayNameForm />
      <ChangePasswordForm />
      <SessionsPanel />
    </NSpace>
  </div>
</template>

<style scoped>
.profile-panels {
  max-width: 720px;
}
</style>
