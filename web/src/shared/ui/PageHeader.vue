<script setup lang="ts">
// Shared page header contract: identity, breadcrumb, title, badges, meta, description and
// actions follow .page-head in shared/styles/main.css. Naive controls stay in the slots.
</script>

<template>
  <div class="page-head">
    <div v-if="$slots.identity" class="page-head__identity">
      <slot name="identity" />
    </div>
    <div class="page-head__main">
      <div v-if="$slots.breadcrumb" class="page-head__breadcrumb">
        <slot name="breadcrumb" />
      </div>
      <div class="page-head__heading">
        <h1><slot name="title" /></h1>
        <div v-if="$slots.badges" class="page-head__badges">
          <slot name="badges" />
        </div>
      </div>
      <div v-if="$slots.meta" class="page-head__meta">
        <slot name="meta" />
      </div>
      <p v-if="$slots.description" class="page-desc">
        <slot name="description" />
      </p>
    </div>
    <div v-if="$slots.actions" class="page-actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<style scoped>
.page-head__identity {
  flex-shrink: 0;
}

.page-head__main {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  flex: 1 1 auto;
  min-width: 0;
}

.page-head__heading {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.page-head h1 {
  margin: 0;
  overflow-wrap: anywhere;
}

.page-head__badges {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.page-head__meta {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  line-height: var(--leading-body);
  color: var(--muted);
  overflow-wrap: anywhere;
}
</style>
