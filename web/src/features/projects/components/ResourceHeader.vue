<script setup lang="ts">
import { BrandIcon } from "@/features/add-resource";
import type { BrandMark } from "@/features/add-resource";
import PageHeader from "@/shared/ui/PageHeader.vue";

const props = withDefaults(
  defineProps<{
    title: string;
    resourceId: string;
    brand?: BrandMark | null;
  }>(),
  { brand: null },
);
</script>

<template>
  <PageHeader>
    <template #identity>
      <BrandIcon v-if="props.brand" v-bind="props.brand" size="lg" />
      <slot v-else name="identity" />
    </template>
    <template #title>{{ props.title }}</template>
    <template v-if="$slots.badges" #badges>
      <slot name="badges" />
    </template>
    <template #meta>{{ props.resourceId }}</template>
    <template v-if="$slots.actions" #actions>
      <slot name="actions" />
    </template>
  </PageHeader>
</template>
