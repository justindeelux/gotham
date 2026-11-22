<script setup lang="ts">
import { NButton } from "naive-ui";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { getEnabledOAuthProviders } from "@/features/auth/api/oauthProviders";
import type { OAuthProviderName } from "@/features/auth/api/oauthProviders";

interface Props {
  mode: "signin" | "signup";
}

const props = defineProps<Props>();
const { t } = useI18n();

const providers = ref<OAuthProviderName[]>([]);

onMounted(async () => {
  try {
    providers.value = await getEnabledOAuthProviders();
  } catch {
    providers.value = [];
  }
});

function label(provider: OAuthProviderName): string {
  return t(props.mode === "signup" ? "auth.oauth.signup" : "auth.oauth.signin", {
    provider: t(`auth.oauth.providers.${provider}`),
  });
}
</script>

<template>
  <div v-if="providers.length > 0" class="oauth-block">
    <div class="auth-divider" aria-hidden="true">
      <span>
        {{ t(mode === "signup" ? "auth.register.divider" : "auth.login.divider") }}
      </span>
    </div>
    <div class="oauth-buttons">
      <NButton
        v-for="provider in providers"
        :key="provider"
        tag="a"
        :href="`/api/v1/auth/oauth/${provider}/login`"
        block
      >
        <template #icon>
          <svg
            v-if="provider === 'github'"
            viewBox="0 0 16 16"
            fill="currentColor"
            aria-hidden="true"
            class="oauth-icon"
          >
            <path
              d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8Z"
            />
          </svg>
          <svg
            v-else
            viewBox="0 0 24 24"
            fill="currentColor"
            aria-hidden="true"
            class="oauth-icon"
          >
            <path
              d="M12.24 10.285V14.4h6.806c-.275 1.765-2.056 5.174-6.806 5.174-4.095 0-7.439-3.389-7.439-7.574s3.344-7.574 7.439-7.574c2.33 0 3.891.989 4.785 1.849l3.254-3.138C18.189 1.186 15.479 0 12.24 0c-6.635 0-12 5.365-12 12s5.365 12 12 12c6.926 0 11.52-4.869 11.52-11.726 0-.788-.085-1.39-.189-1.989H12.24z"
            />
          </svg>
        </template>
        {{ label(provider) }}
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.oauth-block {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin-top: var(--space-1);
}

.oauth-buttons {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.oauth-icon {
  width: var(--text-base);
  height: var(--text-base);
}

.auth-divider {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--text-sm);
  color: var(--muted);
  white-space: nowrap;
}

.auth-divider::before,
.auth-divider::after {
  content: "";
  height: 1px;
  flex: 1 1 0;
  background: var(--border);
}
</style>
