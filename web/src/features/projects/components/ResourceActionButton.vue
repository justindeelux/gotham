<script setup lang="ts">
import { NButton, NIcon, NTooltip } from "naive-ui";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import type { IconName } from "@/shared/ui/GothamIcon.vue";

type ActionType = "default" | "primary" | "warning" | "error" | "success";

interface Props {
  label: string;
  icon: IconName;
  type?: ActionType;
  loading?: boolean;
  disabled?: boolean;
  disabledReason?: string | null;
}

const props = withDefaults(defineProps<Props>(), {
  type: "default",
  loading: false,
  disabled: false,
  disabledReason: null,
});

const emit = defineEmits<{ click: [] }>();
</script>

<template>
  <NTooltip trigger="hover" :disabled="props.disabledReason === null">
    <template #trigger>
      <NButton
        ghost
        :type="props.type"
        :loading="props.loading"
        :disabled="props.disabled || props.disabledReason !== null"
        @click="emit('click')"
      >
        <template #icon>
          <NIcon><GothamIcon :name="props.icon" /></NIcon>
        </template>
        {{ props.label }}
      </NButton>
    </template>
    {{ props.disabledReason }}
  </NTooltip>
</template>
