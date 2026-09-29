<script setup lang="ts">
import { NAlert, NButton, NCard, NSpin, NText } from "naive-ui";
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { acceptInvite, describeTeamError } from "../api/teams";

/**
 * Invite acceptance: the page the one-time link points at.
 *
 * The recipient posts the token from the query string to
 * `POST /v1/invites/accept` (never a URL path) as the signed-in account whose
 * email the invite names. The token is consumed once and is not stored.
 */
const route = useRoute();
const router = useRouter();

const token = ref<string>(String(route.query.token ?? ""));
const accepting = ref(false);
const error = ref<string | null>(null);
const joined = ref("");

async function handleAccept(): Promise<void> {
  accepting.value = true;
  error.value = null;
  try {
    const team = await acceptInvite(token.value);
    joined.value = team.name;
  } catch (err) {
    error.value = describeTeamError(err);
  } finally {
    accepting.value = false;
  }
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
    <NCard title="Team invite">
      <NSpin :show="accepting">
        <div class="stack">
          <NAlert v-if="joined" type="success" :show-icon="true">
            You joined {{ joined }}.
          </NAlert>
          <NAlert v-else-if="!token" type="warning" :show-icon="true">
            This link carries no invite token. Open the link from the invite
            exactly as it was shared.
          </NAlert>
          <NAlert v-else-if="error" type="error" :show-icon="true">
            {{ error }}
          </NAlert>
          <NText v-if="!joined" depth="3">
            Accepting uses the session you are signed in with — sign in with
            the invited address. Invites are single-use.
          </NText>
          <NButton type="primary" @click="goToTeams">
            {{ joined ? "Open teams" : "Back to teams" }}
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
