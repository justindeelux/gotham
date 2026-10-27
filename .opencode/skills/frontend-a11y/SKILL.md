---
name: frontend-a11y
description: >
  Accessibility patterns for Vue 3 and Naive UI — semantic HTML, ARIA attributes,
  form labeling, keyboard navigation, focus management, and screen reader support.
  Use when building any interactive UI component or form.
metadata:
  origin: community
---

# Frontend Accessibility Patterns

Practical accessibility patterns for Vue 3 and Naive UI. Covers the issues most commonly flagged in code review: missing form labels, incorrect ARIA usage, non-semantic interactive elements, and broken keyboard navigation.

## When to Activate

- Building or reviewing form components (`<input>`, `<select>`, `<textarea>`)
- Creating interactive elements (modals, dropdowns, tooltips, tabs)
- Using `<div>` or `<span>` with `@click`
- Adding `aria-*` attributes to any element
- Implementing keyboard navigation or focus management
- Receiving accessibility feedback from code review tools (CodeRabbit, ESLint a11y)
- Building components that must support screen readers

## Form Accessibility

Missing `for` / `id` pairing and disconnected error messages are the most common issues flagged in code review.

### Label Connection

```vue
<template>
<!-- BAD: label has no connection to input — screen readers cannot associate them -->
<label>Email</label>
<input type="email" />

<!-- GOOD: for matches input id -->
<label for="email">Email</label>
<input id="email" type="email" />
</template>
```

### Required Fields

```vue
<template>
<!-- BAD: visual-only asterisk conveys nothing to screen readers -->
<label for="email">Email *</label>
<input id="email" type="email" />

<!-- GOOD: required enables native browser validation; aria-required signals it to screen readers -->
<label for="email">
  Email <span aria-hidden="true">*</span>
</label>
<input id="email" type="email" required aria-required="true" />
</template>
```

### Error Messages

```vue
<template>
<!-- BAD: error text exists visually but is not linked to the input -->
<input id="email" />
<span class="error">Invalid email address</span>

<!-- GOOD: description connects the error; invalid exposes the field state -->
<input
  id="email"
  type="email"
  aria-describedby="email-error"
  :aria-invalid="Boolean(error)"
/>
<span v-if="error" id="email-error" role="alert">{{ error }}</span>
</template>
```

### Complete Accessible Form

Use Naive UI's form components and adapt Zod schemas through `ruleFrom`. Keep
the schema in the feature's `schemas/` directory.

```typescript
// schemas/login.ts
import { z } from "zod";

export const loginSchema = z.object({
  email: z.email(),
  password: z.string().min(1, "Password is required"),
});
```

```vue
<script setup lang="ts">
import { computed, ref } from "vue";
import { NButton, NForm, NFormItem, NInput } from "naive-ui";
import type { FormInst } from "naive-ui";
import { fieldErrors, ruleFrom } from "@/shared/validation/naiveAdapter";
import { loginSchema } from "./schemas/login";

const emit = defineEmits<{
  submit: [email: string, password: string];
}>();
const formRef = ref<FormInst | null>(null);
const model = ref({ email: "", password: "" });
const submitted = ref(false);
const errors = computed(() => ({
  email: submitted.value ? fieldErrors(loginSchema.shape.email, model.value.email) : [],
  password: submitted.value ? fieldErrors(loginSchema.shape.password, model.value.password) : [],
}));
const rules = {
  email: ruleFrom(loginSchema.shape.email, { required: true }),
  password: ruleFrom(loginSchema.shape.password, { required: true }),
};

async function handleSubmit(): Promise<void> {
  if (!formRef.value) return;
  submitted.value = true;
  try {
    await formRef.value.validate();
  } catch {
    return; // The form displays validation errors; do not submit invalid input.
  }
  emit("submit", model.value.email, model.value.password);
}
</script>

<template>
  <n-form ref="formRef" :model="model" :rules="rules" @submit.prevent="handleSubmit">
    <n-form-item label="Email" :label-props="{ for: 'email' }" path="email">
      <n-input
        v-model:value="model.email"
        :input-props="{
          id: 'email',
          'aria-required': true,
          'aria-describedby': 'email-error',
          'aria-invalid': errors.email.length > 0,
        }"
        autocomplete="email"
      />
      <template #feedback>
        <span id="email-error" role="alert">{{ errors.email[0] ?? "" }}</span>
      </template>
    </n-form-item>
    <n-form-item label="Password" :label-props="{ for: 'password' }" path="password">
      <n-input
        v-model:value="model.password"
        type="password"
        :input-props="{
          id: 'password',
          'aria-required': true,
          'aria-describedby': 'password-error',
          'aria-invalid': errors.password.length > 0,
        }"
        autocomplete="current-password"
      />
      <template #feedback>
        <span id="password-error" role="alert">{{ errors.password[0] ?? "" }}</span>
      </template>
    </n-form-item>
    <n-button attr-type="submit">Log in</n-button>
  </n-form>
</template>
```

Verify that the rendered controls expose their labels and validation errors.
Use the project's i18n pattern for labels and messages in production.

## Semantic HTML

Use the element that matches the intent. Screen readers and keyboard users depend on native semantics.

```vue
<template>
<!-- BAD: div has no role, no keyboard support, no accessible name -->
<div @click="handleClick">Submit</div>

<!-- GOOD: button is focusable, activates on Enter/Space, announces as "button" -->
<button type="button" @click="handleClick">Submit</button>
</template>
```

```vue
<template>
<!-- BAD: non-semantic navigation -->
<div @click="router.push('/home')">Home</div>

<!-- GOOD: anchor supports right-click, middle-click, and keyboard navigation -->
<a href="/home">Home</a>
</template>
```

```vue
<template>
<!-- BAD: heading hierarchy skipped (h1 to h4) -->
<h1>Dashboard</h1>
<h4>Recent Activity</h4>

<!-- GOOD: sequential heading levels -->
<h1>Dashboard</h1>
<h2>Recent Activity</h2>
</template>
```

## ARIA Attributes

Use ARIA only when native HTML semantics are insufficient. Wrong ARIA is worse than no ARIA.

### aria-label vs aria-labelledby

```vue
<template>
<!-- aria-label: inline string label — use when no visible label text exists -->
<button aria-label="Close modal">
  <XIcon />
</button>

<!-- aria-labelledby: references another element's text — use when a visible label exists -->
<section aria-labelledby="section-title">
  <h2 id="section-title">Recent Orders</h2>
  <!-- content -->
</section>
</template>
```

### aria-describedby

```vue
<template>
<!-- Provides supplementary description beyond the label -->
<button
  aria-describedby="delete-warning"
  @click="handleDelete"
> Delete account
</button>
<p id="delete-warning">This action cannot be undone.</p>
</template>
```

### aria-live for Dynamic Content

```vue
<script setup lang="ts">
interface Props {
  message: string;
  isError?: boolean;
}
defineProps<Props>();
</script>

<template>
  <!-- Keep the live region mounted; polite waits, assertive interrupts. -->
  <div :role="isError ? 'alert' : 'status'" :aria-live="isError ? 'assertive' : 'polite'" aria-atomic="true">
    {{ message }}
  </div>
</template>
```

### aria-expanded and aria-controls

```vue
<script setup lang="ts">
import { ref, useId } from "vue";

defineProps<{ title: string }>();
const isOpen = ref(false);
const contentId = useId();
</script>

<template>
  <div>
    <button
      type="button"
      :aria-expanded="isOpen"
      :aria-controls="contentId"
      @click="isOpen = !isOpen"
    >
      {{ title }}
    </button>
    <div :id="contentId" :hidden="!isOpen">
      <slot />
    </div>
  </div>
</template>
```

## Keyboard Navigation

Every interactive element must be reachable and operable by keyboard alone.

### Dropdown

Use the existing Naive UI select/dropdown rather than rebuilding its keyboard model.

```vue
<script setup lang="ts">
import { computed, ref } from "vue";
import { NSelect } from "naive-ui";

const props = defineProps<{ options: string[] }>();
const emit = defineEmits<{ select: [value: string] }>();
const selected = ref<string | null>(null);
const selectOptions = computed(() =>
  props.options.map((value) => ({ label: value, value })),
);

function handleSelect(value: string): void {
  selected.value = value;
  emit("select", value);
}
</script>

<template>
  <n-select
    v-if="selectOptions.length"
    :value="selected"
    :options="selectOptions"
    aria-label="Choose an option"
    @update:value="handleSelect"
  />
</template>
```

Verify ArrowUp/ArrowDown navigation, Enter/Space activation, Escape dismissal,
and Tab movement on the rendered control. A custom listbox also needs an active
option, `aria-expanded`, `aria-controls`, and `aria-selected`; a click handler alone
is insufficient.

## Focus Management

Focus must move logically when UI state changes — especially for modals and route transitions.

### Modal Focus Restoration

Use Naive UI's modal focus trap and initial focus behavior. Restore the invoking
element after the closing transition, if it still exists.

```vue
<script setup lang="ts">
import { watch } from "vue";
import { NModal } from "naive-ui";

interface Props {
  isOpen: boolean;
  title: string;
}
const props = defineProps<Props>();
const emit = defineEmits<{ close: [] }>();
let previousFocus: HTMLElement | null = null;

watch(() => props.isOpen, (isOpen) => {
  if (isOpen) {
    previousFocus = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  }
});

function restoreFocus(): void {
  if (previousFocus?.isConnected) previousFocus.focus();
}
</script>

<template>
  <n-modal
    :show="isOpen"
    preset="dialog"
    :title="title"
    :auto-focus="true"
    :trap-focus="true"
    @update:show="(show) => { if (!show) emit('close'); }"
    @after-leave="restoreFocus"
  >
    <slot />
  </n-modal>
</template>
```

Verify Tab/Shift+Tab remain inside, Escape follows the dialog's dismissal policy,
and focus returns to the trigger. Use the shared bounded modal layout.

## Images and Icons

```vue
<template>
<!-- BAD: decorative icon announced as unlabeled image -->
<img src="/icon.svg" />

<!-- GOOD: decorative image hidden from screen readers -->
<img src="/decoration.png" alt="" aria-hidden="true" />

<!-- GOOD: meaningful image with descriptive alt text -->
<img src="/chart.png" alt="Monthly revenue increased 23% from January to March" />

<!-- GOOD: icon button with accessible label -->
<button aria-label="Delete item">
  <TrashIcon aria-hidden="true" />
</button>
</template>
```

## Reduced Motion

Respect users who have requested reduced motion in their OS settings. VueUse already
provides a reactive media-query preference.

```vue
<script setup lang="ts">
import { usePreferredReducedMotion } from "@vueuse/core";

const reducedMotion = usePreferredReducedMotion();
</script>

<template>
  <div :class="{ 'reduce-motion': reducedMotion === 'reduce' }">
    <slot />
  </div>
</template>

<style scoped>
.reduce-motion {
  transition: none;
  animation: none;
}
</style>
```

## Anti-Patterns

```vue
<template>
<!-- BAD: @click on non-interactive element with no keyboard support -->
<div @click="handleClick">Click me</div>

<!-- BAD: aria-label on a div that has no role -->
<div aria-label="Navigation">...</div>

<!-- BAD: placeholder used as a substitute for label -->
<input placeholder="Enter your email" />

<!-- BAD: positive tabindex creates unpredictable tab order -->
<button :tabindex="3">Submit</button>

<!-- BAD: aria-hidden on a focusable element — keyboard users get trapped -->
<button aria-hidden="true">Open</button>

<!-- BAD: role="button" on div without keyboard handler -->
<div role="button" @click="handleClick">Submit</div>
<!-- Missing: tabindex={0}, @keydown for Enter/Space -->
</template>
```

## Checklist

Before submitting any interactive component for review:

- [ ] Every `<input>`, `<select>`, and `<textarea>` has a connected `<label>` via `for`/`id`
- [ ] Error messages are linked with `aria-describedby` and marked `role="alert"`
- [ ] No `@click` on `<div>` or `<span>` without `role`, `tabindex`, and `@keydown`
- [ ] Icon-only buttons have `aria-label`
- [ ] Decorative images use `alt=""` and `aria-hidden="true"`
- [ ] Modals restore focus on close (for full focus trapping with Tab/Shift+Tab cycling, use Naive UI's modal focus trap)
- [ ] Dynamic content updates use `aria-live`
- [ ] `prefers-reduced-motion` is respected for animations

## Related Skills

- `vue` — Vue component, state, and testing patterns
- `design-system` — design token and component consistency
