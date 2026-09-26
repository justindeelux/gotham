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
} from "naive-ui";
import type { FormInst, FormItemRule, FormRules } from "naive-ui";
import { reactive, ref } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import { describeAuthError, useAuthStore } from "../stores/auth";

interface RegisterForm {
  email: string;
  password: string;
  confirmPassword: string;
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
});

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
      min: 8,
      message: "Password must be at least 8 characters",
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
};

/** redirectAfterAuth honours ?redirect when it is a safe local path. */
async function redirectAfterAuth(): Promise<void> {
  const redirect = route.query.redirect;
  const target =
    typeof redirect === "string" && redirect.startsWith("/") && !redirect.startsWith("//")
      ? redirect
      : "/dashboard";
  await router.replace(target);
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
    await authStore.register(form.email, form.password);
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
    <NCard class="auth-card" title="Create your Gotham account">
      <NSpace vertical :size="16">
        <NAlert v-if="errorMessage" type="error" :show-icon="true">
          {{ errorMessage }}
        </NAlert>

        <NForm ref="formRef" :model="form" :rules="rules" @submit.prevent="handleSubmit">
          <NFormItem label="Email" path="email">
            <NInput
              v-model:value="form.email"
              placeholder="you@example.com"
              :input-props="{ autocomplete: 'email', type: 'email' }"
              @keyup.enter="handleSubmit"
            />
          </NFormItem>

          <NFormItem label="Password" path="password">
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              placeholder="At least 8 characters"
              :input-props="{ autocomplete: 'new-password' }"
              @keyup.enter="handleSubmit"
            />
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

          <NSpace vertical :size="12">
            <NButton
              type="primary"
              block
              :loading="submitting"
              @click="handleSubmit"
            >
              Create account
            </NButton>
            <NButton
              tag="a"
              href="/api/v1/auth/oauth/github/login"
              block
              quaternary
            >
              Sign up with GitHub
            </NButton>
          </NSpace>
        </NForm>

        <NText depth="3">
          Already have an account?
          <RouterLink to="/login">Sign in</RouterLink>
        </NText>
      </NSpace>
    </NCard>
  </div>
</template>
