<script setup lang="ts">
import { NButton } from "naive-ui";
import { RouterLink } from "vue-router";
</script>

<template>
  <div class="denied-page">
    <p class="eyebrow">{{ $t("denied.eyebrow") }}</p>
    <h1>{{ $t("denied.title") }}</h1>
    <p class="page-desc">{{ $t("denied.body") }}</p>
    <RouterLink :to="{ name: 'dashboard' }" custom>
      <template #default="{ navigate }">
        <NButton type="primary" @click="navigate">
          {{ $t("denied.back") }}
        </NButton>
      </template>
    </RouterLink>
  </div>
</template>

<style scoped>
.denied-page {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-2);
  max-width: 72ch;
  padding: var(--space-8) 0;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--tracking-eyebrow);
  text-transform: uppercase;
  color: var(--muted);
  margin: 0;
}

.denied-page h1 {
  font-size: var(--text-2xl);
  line-height: var(--leading-title);
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  margin: 0 0 var(--space-4);
}
</style>
