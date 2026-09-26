<script setup lang="ts">
import { NCard, NEmpty, NSpace, NText } from "naive-ui";
import { computed, onMounted } from "vue";

import { useAuthStore } from "../stores/auth";

const authStore = useAuthStore();

const greeting = computed<string>(() =>
  authStore.user?.email ? `Signed in as ${authStore.user.email}` : "Signed in",
);

onMounted(async () => {
  if (!authStore.user) {
    try {
      await authStore.fetchMe();
    } catch {
      // The HTTP interceptor already handles a dead session.
    }
  }
});
</script>

<template>
  <NSpace vertical :size="16">
    <NCard title="Dashboard">
      <NSpace vertical :size="16">
        <NText depth="2">{{ greeting }}</NText>
        <NEmpty description="No data yet">
          <template #extra>
            <NText depth="3">
              Your applications, databases, and services will appear here once
              you create them.
            </NText>
          </template>
        </NEmpty>
      </NSpace>
    </NCard>
  </NSpace>
</template>
