<script setup lang="ts">
import {
  NAlert,
  NButton,
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

import AuthFootnote from "@/features/auth/components/AuthFootnote.vue";
import GitHubOAuthButton from "@/features/auth/components/GitHubOAuthButton.vue";
import PasswordStrengthMeter from "@/features/auth/components/PasswordStrengthMeter.vue";
import { useRegisterInvite } from "@/features/auth/composables/useRegisterInvite";
import { describeAuthError, useAuthStore } from "@/features/auth/stores/auth";
import { authSwitchTarget, safeRedirect } from "@/features/auth/utils/authRedirect";
import {
  meetsPasswordPolicy,
  strengthOf,
} from "@/features/auth/utils/passwordStrength";

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
const invite = useRegisterInvite();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const errorMessage = ref("");
const form = reactive<RegisterForm>({
  email: "",
  password: "",
  confirmPassword: "",
  terms: false,
});

onMounted(async () => {
  const raw = route.query.invite;
  const token = typeof raw === "string" ? raw : "";

  // Take the token from the URL and block submission BEFORE the config
  // request: on a slow connection the form is already visible, and a submit
  // during that window would otherwise omit the invite and answer 403.
  if (token) {
    invite.holdToken(token);
  }

  await authStore.fetchAuthConfig();

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

  const valid = await invite.acceptInvite();
  if (!valid) {
    void router.replace({ name: "login" });
    return;
  }
  if (invite.inviteEmail.value && !form.email) {
    form.email = invite.inviteEmail.value;
  }
});

const strength = computed<number>(() => strengthOf(form.password));

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
        meetsPasswordPolicy(value),
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
      invite.inviteToken.value || undefined,
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
    <div class="auth-card">
      <NSpace vertical :size="16">
        <nav
          v-if="authStore.registrationOpen"
          class="auth-switch"
          aria-label="Sign in or create an account"
        >
          <RouterLink :to="authSwitchTarget(route, 'login')">
            Sign in
          </RouterLink>
          <RouterLink to="/register" class="is-active" aria-current="page">
            Create account
          </RouterLink>
        </nav>

        <div>
          <h2 class="auth-title">Create account</h2>
          <NText v-if="invite.inviteToken.value" depth="3">
            You were invited to join
            <span class="mono">{{ invite.inviteTeam.value || "this team" }}</span
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
          <NFormItem label="Email" path="email" :label-props="{ for: 'register-email' }">
            <NInput
              v-model:value="form.email"
              placeholder="you@gotham.dev"
              :input-props="{ id: 'register-email', autocomplete: 'email', type: 'email' }"
              @keyup.enter="handleSubmit"
            />
          </NFormItem>

          <div class="form-row">
            <NFormItem label="Password" path="password" :label-props="{ for: 'register-password' }">
              <NSpace vertical :size="8" class="password-field">
                <NInput
                  v-model:value="form.password"
                  type="password"
                  show-password-on="click"
                  placeholder="At least 10 characters"
                  :input-props="{ id: 'register-password', autocomplete: 'new-password' }"
                  @keyup.enter="handleSubmit"
                />
                <PasswordStrengthMeter :score="strength" />
                <span class="field-hint">
                  At least 10 characters with 2 character classes: lowercase,
                  uppercase, digits, symbols.
                </span>
              </NSpace>
            </NFormItem>

            <NFormItem
              label="Confirm password"
              path="confirmPassword"
              :label-props="{ for: 'register-confirm-password' }"
            >
              <NInput
                v-model:value="form.confirmPassword"
                type="password"
                show-password-on="click"
                placeholder="Repeat your password"
                :input-props="{ id: 'register-confirm-password', autocomplete: 'new-password' }"
                @keyup.enter="handleSubmit"
              />
            </NFormItem>
          </div>

          <NFormItem path="terms" :show-label="false">
            <NCheckbox v-model:checked="form.terms">
              I agree to the Terms of Use and to how this instance stores data.
            </NCheckbox>
          </NFormItem>

          <NButton
            type="primary"
            block
            :loading="submitting"
            :disabled="invite.inviteChecking.value"
            @click="handleSubmit"
          >
            Create account
          </NButton>

          <div class="auth-divider" aria-hidden="true">
            <span>or</span>
          </div>

          <template v-if="!invite.inviteToken.value">
            <GitHubOAuthButton mode="signup" />
          </template>
        </NForm>

        <AuthFootnote />
      </NSpace>
    </div>
  </div>
</template>

<style scoped>
/* Ported from docs/design/assets/gotham-views.css: the design's card is a bare
   width limiter — no border, no background. The only filled element is the
   tab switch below. */
.auth-card {
  width: min(420px, 100%);
}

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

.password-field {
  width: 100%;
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
</style>
