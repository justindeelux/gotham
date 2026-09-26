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
  useMessage,
} from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { onMounted, reactive, ref } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import { describeAuthError, useAuthStore } from "../stores/auth";

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
    {
      min: 8,
      message: "Password must be at least 8 characters",
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
    await authStore.login(form.email, form.password);
    await redirectAfterAuth();
  } catch (error) {
    errorMessage.value = describeAuthError(error);
  } finally {
    submitting.value = false;
  }
}

onMounted(() => {
  if (route.query.error === "oauth_failed") {
    message.error("GitHub sign-in failed. Please try again.");
    void router.replace({ path: "/login" });
  }
});
</script>

<template>
  <div class="auth-page">
    <NCard class="auth-card" title="Sign in to Gotham">
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
              placeholder="Your password"
              :input-props="{ autocomplete: 'current-password' }"
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
              Sign in
            </NButton>
            <NButton
              tag="a"
              href="/api/v1/auth/oauth/github/login"
              block
              quaternary
            >
              Sign in with GitHub
            </NButton>
          </NSpace>
        </NForm>

        <NText depth="3">
          Need an account?
          <RouterLink to="/register">Create one</RouterLink>
        </NText>
      </NSpace>
    </NCard>
  </div>
</template>
