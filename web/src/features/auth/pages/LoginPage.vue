<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NSpace,
  NText,
  useMessage,
} from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, onBeforeRouteLeave, useRoute, useRouter } from "vue-router";

import { describeAuthError, useAuthStore } from "@/features/auth/stores/auth";
import { loginRules } from "@/features/auth/schemas/auth";
import { authSwitchTarget, safeRedirect } from "@/features/auth/utils/authRedirect";
import { createVisibleValidation } from "@/features/auth/utils/visibleValidation";
import { onLocaleChange } from "@/shared/i18n";
import AuthFootnote from "@/features/auth/components/AuthFootnote.vue";
import GitHubOAuthButton from "@/features/auth/components/GitHubOAuthButton.vue";
import MfaChallengeForm from "@/features/auth/components/MfaChallengeForm.vue";

interface LoginForm {
  email: string;
  password: string;
}

const { t } = useI18n();
const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();
const message = useMessage();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const rawError = ref<unknown>(null);
const errorMessage = computed<string>(() =>
  rawError.value === null ? "" : describeAuthError(rawError.value),
);
const form = reactive<LoginForm>({ email: "", password: "" });

const step = ref<"credentials" | "code">("credentials");

const mfaNotice = ref("");

const rules: FormRules = loginRules();

const visible = createVisibleValidation();
const trackedRules: FormRules = visible.trackRules(rules);
const stopLocaleWatch = onLocaleChange(() => {
  visible.refreshVisible(formRef);
});

onUnmounted(() => {
  stopLocaleWatch();
  authStore.clearMfaChallenge();
});

onBeforeRouteLeave(() => {
  authStore.clearMfaChallenge();
});

async function redirectAfterAuth(): Promise<void> {
  await router.replace(safeRedirect(route.query.redirect) ?? "/dashboard");
}

async function handleSubmit(): Promise<void> {
  rawError.value = null;
  mfaNotice.value = "";

  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  submitting.value = true;
  try {
    const { mfaRequired } = await authStore.login(form.email, form.password);
    if (mfaRequired) {
      step.value = "code";
      form.password = "";
      return;
    }
    await redirectAfterAuth();
  } catch (error) {
    rawError.value = error;
  } finally {
    submitting.value = false;
  }
}

function backToCredentials(): void {
  authStore.clearMfaChallenge();
  step.value = "credentials";
}

function handleChallengeExpired(): void {
  authStore.clearMfaChallenge();
  rawError.value = null;
  mfaNotice.value = t("auth.mfa.sessionExpired");
  step.value = "credentials";
}

onMounted(() => {
  void authStore.fetchAuthConfig();

  if (authStore.mfaPending) {
    if (authStore.isMfaChallengeLive()) {
      step.value = "code";
    } else {
      authStore.clearMfaChallenge();
      mfaNotice.value = t("auth.mfa.sessionExpired");
    }
  }

  if (route.query.error === "oauth_failed") {
    message.error(t("auth.login.oauthFailed"));
    void router.replace({ path: "/login" });
  }
});
</script>

<template>
  <div class="auth-page">
    <div class="auth-card">
      <NSpace vertical :size="16">
        <nav
          v-if="authStore.registrationOpen"
          class="auth-switch"
          :aria-label="t('auth.login.switchLabel')"
        >
          <RouterLink to="/login" class="is-active" aria-current="page">
            {{ t("auth.login.title") }}
          </RouterLink>
          <RouterLink :to="authSwitchTarget(route, 'register')">
            {{ t("auth.login.createAccount") }}
          </RouterLink>
        </nav>

        <div>
          <h2 class="auth-title">
            {{ step === "code" ? t("auth.mfa.title") : t("auth.login.title") }}
          </h2>
          <NText depth="3">
            {{ step === "code" ? t("auth.mfa.subtitle") : t("auth.login.subtitle") }}
          </NText>
        </div>

        <MfaChallengeForm
          v-if="step === 'code'"
          @success="redirectAfterAuth"
          @cancel="backToCredentials"
          @expired="handleChallengeExpired"
        />

        <template v-else>
          <NAlert v-if="mfaNotice" type="warning" :show-icon="true">
            {{ mfaNotice }}
          </NAlert>

          <NAlert v-if="errorMessage" type="error" :show-icon="true">
            {{ errorMessage }}
          </NAlert>

          <NForm ref="formRef" :model="form" :rules="trackedRules" @submit.prevent="handleSubmit">
            <NFormItem :label="t('auth.login.emailLabel')" path="email" :label-props="{ for: 'login-email' }">
              <NInput
                v-model:value="form.email"
                :placeholder="t('auth.login.emailPlaceholder')"
                :input-props="{ id: 'login-email', autocomplete: 'username', type: 'email' }"
                @keyup.enter="handleSubmit"
              />
            </NFormItem>

            <NFormItem :label="t('auth.login.passwordLabel')" path="password" :label-props="{ for: 'login-password' }">
              <NInput
                v-model:value="form.password"
                type="password"
                show-password-on="click"
                :placeholder="t('auth.login.passwordPlaceholder')"
                :input-props="{ id: 'login-password', autocomplete: 'current-password' }"
                @keyup.enter="handleSubmit"
              />
            </NFormItem>

            <NButton
              type="primary"
              block
              :loading="submitting"
              @click="handleSubmit"
            >
              {{ t("auth.login.submit") }}
            </NButton>

            <p class="auth-contact">{{ t("auth.login.contactAdmin") }}</p>

            <div class="auth-divider" aria-hidden="true">
              <span>{{ t("auth.login.divider") }}</span>
            </div>

            <GitHubOAuthButton mode="signin" />
          </NForm>
        </template>

        <AuthFootnote />
      </NSpace>
    </div>
  </div>
</template>

<style scoped>
.auth-card {
  width: min(420px, 100%);
}

.auth-title {
  margin: 0 0 4px;
  font-size: 20px;
  font-weight: 700;
  color: var(--fg-2);
}

.auth-switch {
  display: flex;
  gap: var(--space-1);
  background: var(--surface-warm);
  border-radius: var(--radius-sm);
  padding: 3px;
}

.auth-switch a {
  flex: 1 1 0;
  text-align: center;
  padding: 7px;
  border-radius: 3px;
  font-size: var(--text-sm);
  color: var(--muted);
  text-decoration: none;
}

.auth-switch a:hover {
  color: var(--fg-2);
}

.auth-switch a.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
  font-weight: 600;
}

.auth-contact {
  font-size: var(--text-xs);
  color: var(--muted);
  margin: 12px 0 0;
}

.auth-divider {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 4px 0;
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
