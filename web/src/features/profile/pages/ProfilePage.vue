<script setup lang="ts">
import { NSpace } from "naive-ui";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import ChangePasswordForm from "@/features/profile/components/ChangePasswordForm.vue";
import DisplayNameForm from "@/features/profile/components/DisplayNameForm.vue";
import ProfileIdentityCard from "@/features/profile/components/ProfileIdentityCard.vue";
import SessionsPanel from "@/features/profile/components/SessionsPanel.vue";
import TotpCard from "@/features/profile/components/TotpCard.vue";

const { t } = useI18n();
const authStore = useAuthStore();

onMounted(() => {
  if (authStore.isAuthenticated) {
    void authStore.fetchMe().catch(() => {});
  }
});

</script>

<template>
  <div class="profile-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("profile.page.eyebrow") }}</p>
        <h1>{{ t("profile.page.title") }}</h1>
        <p class="page-desc">
          {{ t("profile.page.description") }}
        </p>
      </div>
    </div>

    <NSpace vertical :size="16" class="profile-panels">
      <ProfileIdentityCard />
      <DisplayNameForm />
      <ChangePasswordForm />
      <TotpCard />
      <SessionsPanel />
    </NSpace>
  </div>
</template>

<style scoped>
.profile-panels {
  max-width: 720px;
}
</style>
