<script setup lang="ts">
import { NButton, NEmpty, NText } from "naive-ui";
import { RouterLink } from "vue-router";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
</script>

<template>
  <div class="not-found">
    <NEmpty :description="$t('not-found.title')">
      <template #icon>
        <GothamIcon name="search" />
      </template>
      <template #extra>
        <div class="not-found-body">
          <NText depth="3">{{ $t("not-found.description") }}</NText>
          <RouterLink :to="{ name: 'dashboard' }">
            <NButton type="primary">{{ $t("not-found.back") }}</NButton>
          </RouterLink>
        </div>
      </template>
    </NEmpty>
  </div>
</template>

<style scoped>
.not-found {
  display: grid;
  place-items: center;
  min-height: 60vh;
}

.not-found-body {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  max-width: 48ch;
  text-align: center;
}
</style>
