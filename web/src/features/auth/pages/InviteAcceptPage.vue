<script setup lang="ts">
import { NAlert, NButton, NCard, NSpin, NText } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { acceptInvite, describeTeamError } from "@/features/teams";
import { stripErrorPrefix } from "@/features/servers";

/**
 * Invite acceptance: the page the one-time link points at.
 *
 * The recipient posts the token from the query string to
 * `POST /v1/invites/accept` (never a URL path) as the signed-in account whose
 * email the invite names. The token is consumed once and is not stored.
 *
 * Failure display derives per locale from the retained raw failure object:
 * the team helper is re-invoked on every evaluation (never a cached
 * translated string, so I18N-8's locale-aware describer stays compatible),
 * classification uses only the raw untranslated message, and a failure with
 * no useful diagnostic renders the single localized fallback.
 */
const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const token = ref<string>(String(route.query.token ?? ""));
const accepting = ref(false);
/** rawFailure retains the original failure; display derives per locale. */
const rawFailure = ref<unknown>(null);
/**
 * error renders the failure for the current locale: failures with no useful
 * raw diagnostic show the single localized fallback (never a duplicated
 * generic summary); otherwise the shared summary heads the team diagnostic,
 * re-derived on every evaluation so a locale-aware team helper is never
 * read from a stale cached string.
 */
const error = computed<string | null>(() => {
  if (rawFailure.value === null) {
    return null;
  }
  if (rawFailureMessage(rawFailure.value) === "") {
    return t("common.errors.unexpected");
  }
  return `${t("common.errors.requestFailed")}: ${describeTeamError(rawFailure.value)}`;
});
const joined = ref("");

/** rawFailureMessage extracts the stripped raw message, or "" when unusable. */
function rawFailureMessage(failure: unknown): string {
  if (typeof failure !== "object" || failure === null) {
    return "";
  }
  const message = (failure as { message?: unknown }).message;
  return typeof message === "string" ? stripErrorPrefix(message).trim() : "";
}

async function handleAccept(): Promise<void> {
  accepting.value = true;
  rawFailure.value = null;
  try {
    const team = await acceptInvite(token.value);
    joined.value = team.name;
    // The token is spent: drop it from the address bar and history so a
    // reload or a shared URL cannot replay it.
    scrubToken();
  } catch (err) {
    rawFailure.value = err;
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
