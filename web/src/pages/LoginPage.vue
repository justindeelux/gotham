<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSpace,
  NText,
  NTooltip,
  useMessage,
} from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { onMounted, reactive, ref } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import { describeAuthError, useAuthStore } from "../stores/auth";
import { authSwitchTarget, safeRedirect } from "../utils/authRedirect";

// Error convention (shared with RegisterPage): client-side validation errors
// render inline on the field via NFormItem; server-side submit failures render
// once in the NAlert above the form, with text from describeAuthError.
interface LoginForm {
  email: string;
  password: string;
}

const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();
const message = useMessage();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const errorMessage = ref("");
const form = reactive<LoginForm>({ email: "", password: "" });

const rules: FormRules = {
  email: [
    { required: true, message: "Email is required", trigger: ["input", "blur"] },
    {
      type: "email",
      message: "Enter a valid email address",
      trigger: ["input", "blur"],
    },
  ],
  password: [
    { required: true, message: "Password is required", trigger: ["input", "blur"] },
  ],
};

/** redirectAfterAuth honours ?redirect when it is a safe local path. */
async function redirectAfterAuth(): Promise<void> {
  await router.replace(safeRedirect(route.query.redirect) ?? "/dashboard");
}

async function handleSubmit(): Promise<void> {
  errorMessage.value = "";

  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  submitting.value = true;
  try {
    await authStore.login(form.email, form.password);
    await redirectAfterAuth();
  } catch (error) {
    errorMessage.value = describeAuthError(error);
  } finally {
    submitting.value = false;
  }
}

onMounted(() => {
  // Registration is open only on a fresh instance; the create-account tab is
  // hidden otherwise and members join through an admin invite link (P-A2).
  void authStore.fetchAuthConfig();

  if (route.query.error === "oauth_failed") {
    message.error("GitHub sign-in failed. Please try again.");
    void router.replace({ path: "/login" });
  }
});
</script>

<template>
  <div class="auth-page">
    <NCard class="auth-card">
      <NSpace vertical :size="16">
        <div
          v-if="authStore.registrationOpen"
          class="auth-switch"
          role="tablist"
          aria-label="Sign in or create an account"
        >
          <RouterLink
            to="/login"
            class="is-active"
            role="tab"
            aria-selected="true"
          >
            Sign in
          </RouterLink>
          <RouterLink
            :to="authSwitchTarget(route, 'register')"
            role="tab"
            aria-selected="false"
          >
            Create account
          </RouterLink>
        </div>

        <div>
          <h2 class="auth-title">Sign in</h2>
          <NText depth="3">
            Use your Gotham team account. Sessions use short-lived JWTs with
            rotating refresh tokens.
          </NText>
        </div>

        <NAlert v-if="errorMessage" type="error" :show-icon="true">
          {{ errorMessage }}
        </NAlert>

        <NForm ref="formRef" :model="form" :rules="rules" @submit.prevent="handleSubmit">
          <NFormItem label="Email" path="email">
            <NInput
              v-model:value="form.email"
              placeholder="you@gotham.dev"
              :input-props="{ autocomplete: 'username', type: 'email' }"
              @keyup.enter="handleSubmit"
            />
          </NFormItem>

          <NFormItem label="Password" path="password">
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              placeholder="Your password"
              :input-props="{ autocomplete: 'current-password' }"
              @keyup.enter="handleSubmit"
            />
          </NFormItem>

          <NButton
            type="primary"
            block
            :loading="submitting"
            @click="handleSubmit"
          >
            Sign in
          </NButton>

          <div class="auth-row">
            <NTooltip trigger="hover">
              <template #trigger>
                <span class="auth-disabled-wrap">
                  <NButton text disabled>Forgot password?</NButton>
                </span>
              </template>
              Password reset is not available yet.
            </NTooltip>
          </div>

          <div class="auth-divider" aria-hidden="true">
            <span>or</span>
          </div>

          <NButton
            tag="a"
            href="/api/v1/auth/oauth/github/login"
            block
          >
            <template #icon>
              <svg
                viewBox="0 0 16 16"
                fill="currentColor"
                aria-hidden="true"
                class="github-icon"
              >
                <path
                  d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8Z"
                />
              </svg>
            </template>
            Sign in with GitHub
          </NButton>
        </NForm>

        <p class="auth-footnote">
          Passwords are hashed with <span class="mono">argon2id</span> · 15-minute
          JWT access tokens with 30-day rotating refresh tokens · GitHub OAuth
          via the <span class="mono">OAuthProvider</span> interface.
        </p>
      </NSpace>
    </NCard>
  </div>
</template>

<style scoped>
/* Tab switch, divider, and footnote ported from docs/design/login.html +
   docs/design/assets/gotham-views.css; tokens come from styles/tokens.css. */
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

.auth-row {
  display: flex;
  justify-content: flex-start;
  margin-top: 12px;
}

.auth-disabled-wrap {
  display: inline-flex;
  cursor: not-allowed;
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

.github-icon {
  width: 16px;
  height: 16px;
}

.auth-footnote {
  margin: 4px 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
  text-align: center;
  line-height: 1.6;
}
</style>
