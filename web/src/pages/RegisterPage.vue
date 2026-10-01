<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NForm,
  NFormItem,
  NInput,
  NSpace,
  NText,
} from "naive-ui";
import type { FormInst, FormItemRule, FormRules } from "naive-ui";
import { computed, onMounted, reactive, ref } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import { describeAuthError, useAuthStore } from "../stores/auth";
import { authSwitchTarget, safeRedirect } from "../utils/authRedirect";

// Error convention (shared with LoginPage): client-side validation errors
// render inline on the field via NFormItem; server-side submit failures render
// once in the NAlert above the form, with text from describeAuthError.
interface RegisterForm {
  email: string;
  password: string;
  confirmPassword: string;
  terms: boolean;
}

const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const errorMessage = ref("");
const form = reactive<RegisterForm>({
  email: "",
  password: "",
  confirmPassword: "",
  terms: false,
});

/**
 * Invite mode (P-A2): when the instance already has an account, registration
 * is closed and this page is reachable only through an admin-created invite
 * link (`/register?invite=<token>`). The token is validated up front so the
 * invitee sees which team they are joining; an unusable token bounces to the
 * sign-in form.
 */
const inviteToken = ref<string>("");
const inviteTeam = ref<string>("");
const inviteEmail = ref<string>("");
const inviteChecking = ref(false);

onMounted(async () => {
  await authStore.fetchAuthConfig();

  const raw = route.query.invite;
  const token = typeof raw === "string" ? raw : "";

  // A fresh instance needs no invite: the first account bootstraps it. An
  // invite present in the URL is always processed, even when ordinary
  // registration happens to be open, so the invitee still joins the team.
  if (!token) {
    if (authStore.registrationOpen) {
      return;
    }
    void router.replace({ name: "login" });
    return;
  }

  inviteToken.value = token;
  inviteChecking.value = true;
  try {
    const info = await authStore.validateInvite(token);
    inviteTeam.value = info.team;
    inviteEmail.value = info.email;
    if (info.email && !form.email) {
      form.email = info.email;
    }
  } catch {
    void router.replace({ name: "login" });
  } finally {
    inviteChecking.value = false;
  }
});

/** countCharClasses counts the character classes present (lower, upper, digit, symbol). */
function countCharClasses(value: string): number {
  let classes = 0;
  if (/[a-z]/.test(value)) {
    classes += 1;
  }
  if (/[A-Z]/.test(value)) {
    classes += 1;
  }
  if (/[0-9]/.test(value)) {
    classes += 1;
  }
  if (/[^A-Za-z0-9]/.test(value)) {
    classes += 1;
  }
  return classes;
}

/** scorePassword rates the password 0-4 following docs/design/login.html. */
function scorePassword(value: string): number {
  let score = 0;
  if (value.length >= 10) {
    score += 1;
  }
  if (value.length >= 14) {
    score += 1;
  }
  if (/[a-z]/.test(value) && /[A-Z]/.test(value)) {
    score += 1;
  }
  if (/[0-9]/.test(value)) {
    score += 1;
  }
  if (/[^A-Za-z0-9]/.test(value)) {
    score += 1;
  }
  return Math.min(4, score);
}

const strengthLabels = ["Not entered", "Very weak", "Weak", "Fair", "Strong"];

const strength = computed<number>(() =>
  form.password ? Math.max(1, scorePassword(form.password)) : 0,
);
const strengthLabel = computed<string>(() => strengthLabels[strength.value]);
const strengthKind = computed<string>(() =>
  strength.value >= 4 ? "on" : strength.value === 3 ? "mid" : "weak",
);

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
    {
      validator: (_rule: FormItemRule, value: string): boolean =>
        value.length >= 10 && countCharClasses(value) >= 2,
      message:
        "Use at least 10 characters with 2 character classes (lowercase, uppercase, digits, symbols)",
      trigger: ["input", "blur"],
    },
  ],
  confirmPassword: [
    { required: true, message: "Please confirm your password", trigger: ["input", "blur"] },
    {
      validator: (_rule: FormItemRule, value: string): boolean =>
        value === form.password,
      message: "Passwords do not match",
      trigger: ["input", "blur"],
    },
  ],
  terms: [
    {
      validator: (_rule: FormItemRule, value: boolean): boolean => value === true,
      message: "You must accept the terms to create an account",
      trigger: ["change"],
    },
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
    await authStore.register(
      form.email,
      form.password,
      inviteToken.value || undefined,
    );
    await redirectAfterAuth();
  } catch (error) {
    errorMessage.value = describeAuthError(error);
  } finally {
    submitting.value = false;
  }
}
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
            :to="authSwitchTarget(route, 'login')"
            role="tab"
            aria-selected="false"
          >
            Sign in
          </RouterLink>
          <RouterLink
            to="/register"
            class="is-active"
            role="tab"
            aria-selected="true"
          >
            Create account
          </RouterLink>
        </div>

        <div>
          <h2 class="auth-title">Create account</h2>
          <NText v-if="inviteToken" depth="3">
            You were invited to join
            <span class="mono">{{ inviteTeam || "this team" }}</span
            >. Choose your credentials to accept.
          </NText>
          <NText v-else depth="3">
            The first account on a new instance becomes the
            <span class="mono">owner</span> of the default team.
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
              :input-props="{ autocomplete: 'email', type: 'email' }"
              @keyup.enter="handleSubmit"
            />
          </NFormItem>

          <NFormItem label="Password" path="password">
            <NSpace vertical :size="8" class="password-field">
              <NInput
                v-model:value="form.password"
                type="password"
                show-password-on="click"
                placeholder="At least 10 characters"
                :input-props="{ autocomplete: 'new-password' }"
                @keyup.enter="handleSubmit"
              />
              <div class="strength-row">
                <span class="strength" aria-hidden="true">
                  <i :class="strength > 0 ? strengthKind : ''" />
                  <i :class="strength > 1 ? strengthKind : ''" />
                  <i :class="strength > 2 ? strengthKind : ''" />
                  <i :class="strength > 3 ? strengthKind : ''" />
                </span>
                <span class="small muted">{{ strengthLabel }}</span>
              </div>
              <span class="field-hint">
                At least 10 characters with 2 character classes: lowercase,
                uppercase, digits, symbols.
              </span>
            </NSpace>
          </NFormItem>

          <NFormItem label="Confirm password" path="confirmPassword">
            <NInput
              v-model:value="form.confirmPassword"
              type="password"
              show-password-on="click"
              placeholder="Repeat your password"
              :input-props="{ autocomplete: 'new-password' }"
              @keyup.enter="handleSubmit"
            />
          </NFormItem>

          <NFormItem path="terms" :show-label="false">
            <NCheckbox v-model:checked="form.terms">
              I agree to the Terms of Use and to how this instance stores data.
            </NCheckbox>
          </NFormItem>

          <NButton
            type="primary"
            block
            :loading="submitting"
            :disabled="inviteChecking"
            @click="handleSubmit"
          >
            Create account
          </NButton>

          <div class="auth-divider" aria-hidden="true">
            <span>or</span>
          </div>

          <template v-if="!inviteToken">
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
              Sign up with GitHub
            </NButton>
          </template>
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
/* Tab switch, strength meter, divider, and footnote ported from
   docs/design/login.html + docs/design/assets/gotham-views.css; tokens come
   from styles/tokens.css. */
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

.password-field {
  width: 100%;
}

.strength-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.strength {
  display: flex;
  gap: 4px;
  flex: 1 1 0;
}

.strength i {
  height: 4px;
  flex: 1 1 0;
  border-radius: var(--radius-pill);
  background: var(--surface-warm);
}

.strength i.on {
  background: var(--success);
}

.strength i.mid {
  background: var(--warn);
}

.strength i.weak {
  background: var(--danger);
}

.small {
  font-size: var(--text-xs);
  white-space: nowrap;
}

.field-hint {
  font-size: var(--text-xs);
  color: var(--muted);
  line-height: 1.6;
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
