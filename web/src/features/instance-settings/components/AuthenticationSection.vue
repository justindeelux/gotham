<script setup lang="ts">
import { NAlert } from "naive-ui";
import { computed, onMounted, ref } from "vue";

import {
  getOAuthProviders,
  oauthProviderNames,
} from "@/features/instance-settings/api/oauthProviders";
import type {
  OAuthProviderInfo,
  OAuthProviderName,
} from "@/features/instance-settings/api/oauthProviders";
import OAuthProviderCard from "@/features/instance-settings/components/OAuthProviderCard.vue";
import { errorText } from "@/shared/api/errors";

interface Props {
  controlPlaneUrl: string;
}

const props = defineProps<Props>();

function emptyProviders(): Record<OAuthProviderName, OAuthProviderInfo> {
  return Object.fromEntries(
    oauthProviderNames.map((name) => [
      name,
      { name, enabled: false, client_id: "", has_secret: false },
    ]),
  ) as Record<OAuthProviderName, OAuthProviderInfo>;
}

const providers = ref<Record<OAuthProviderName, OAuthProviderInfo>>(emptyProviders());
const loadError = ref("");

function isProviderName(name: string): name is OAuthProviderName {
  return (oauthProviderNames as string[]).includes(name);
}

onMounted(async () => {
  try {
    for (const info of await getOAuthProviders()) {
      if (isProviderName(info.name)) {
        providers.value[info.name] = info;
      }
    }
  } catch (error) {
    loadError.value = errorText(error);
  }
});

function apply(info: OAuthProviderInfo): void {
  if (isProviderName(info.name)) {
    providers.value[info.name] = info;
  }
}

const callbackBase = computed<string>(() => {
  const url = props.controlPlaneUrl.trim();
  try {
    return url === "" ? window.location.origin : new URL(url).origin;
  } catch {
    return window.location.origin;
  }
});

function callbackUrl(name: OAuthProviderName): string {
  return `${callbackBase.value}/api/v1/auth/oauth/${name}/callback`;
}
</script>

<template>
  <section class="oauth-section">
    <NAlert v-if="loadError" type="error" :show-icon="true">{{ loadError }}</NAlert>

    <OAuthProviderCard
      v-for="name in oauthProviderNames"
      :key="name"
      :provider="providers[name]"
      :callback-url="callbackUrl(name)"
      @saved="apply"
    />
  </section>
</template>

<style scoped>
.oauth-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
</style>
