<script setup lang="ts">
import { NAlert, NButton, NCard, NSpin, NText } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { acceptInvite, describeTeamError } from "@/features/teams";

/**
 * Invite acceptance: the page the one-time link points at.
 *
 * The recipient posts the token from the query string to
 * `POST /v1/invites/accept` (never a URL path) as the signed-in account whose
 * email the invite names. The token is consumed once and is not stored.
 *
 * Static copy renders through the auth catalog; the team error text itself
 * stays owned by the teams package (I18N-8 localizes its details) and passes
 * through here as the raw diagnostic under the localized page copy.
 */
const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const token = ref<string>(String(route.query.token ?? ""));
const accepting = ref(false);
const rawError = ref<string | null>(null);
const error = computed<string | null>(() => rawError.value);
const joined = ref("");

async function handleAccept(): Promise<void> {
  accepting.value = true;
  rawError.value = null;
  try {
    const team = await acceptInvite(token.value);
    joined.value = team.name;
    // The token is spent: drop it from the address bar and history so a
    // reload or a shared URL cannot replay it.
    scrubToken();
  } catch (err) {
    rawError.value = describeTeamError(err);
  } finally {
    accepting.value = false;
  }
}

/**
 * scrubToken replaces the token-bearing query with the bare accept path. It
 * runs only after the token was consumed, so the signed-out handoff (which
 * carries the token to the login/register redirect) is untouched.
 */
function scrubToken(): void {
  void router.replace({ query: {} });
}

/** goToTeams returns to the teams page after the flow settles. */
function goToTeams(): void {
  void router.push({ name: "teams" });
}

onMounted(() => {
  if (token.value) {
    void handleAccept();
  }
});
</script>

<template>
  <div class="invite-page">
    <NCard :title="t('auth.invite.title')">
      <NSpin :show="accepting">
        <div class="stack">
          <NAlert v-if="joined" type="success" :show-icon="true">
            {{ t("auth.invite.joined", { name: joined }) }}
          </NAlert>
          <NAlert v-else-if="!token" type="warning" :show-icon="true">
            {{ t("auth.invite.noToken") }}
          </NAlert>
          <NAlert v-else-if="error" type="error" :show-icon="true">
            {{ error }}
          </NAlert>
          <NText v-if="!joined" depth="3">
            {{ t("auth.invite.hint") }}
          </NText>
          <NButton type="primary" @click="goToTeams">
            {{ joined ? t("auth.invite.openTeams") : t("auth.invite.backToTeams") }}
          </NButton>
        </div>
      </NSpin>
    </NCard>
  </div>
</template>

<style scoped>
.invite-page {
  max-width: 560px;
}

.stack {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  align-items: flex-start;
}
</style>
