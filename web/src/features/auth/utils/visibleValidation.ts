import type { FormInst, FormItemRule, FormRules } from "naive-ui";
import type { Ref } from "vue";

type RuleValidator = NonNullable<FormItemRule["validator"]>;

export function createVisibleValidation() {
  const failedPaths = new Set<string>();

  function trackRules(rules: FormRules): FormRules {
    const tracked: FormRules = {};
    for (const [path, entry] of Object.entries(rules)) {
      const list: FormItemRule[] = Array.isArray(entry) ? entry : [entry];
      tracked[path] = list.map((rule) => {
        if (typeof rule.validator !== "function") {
          return rule;
        }
        const validate = rule.validator;
        const tracked: RuleValidator = (...args: Parameters<RuleValidator>) => {
          const result = validate(...args);
          if (!(result instanceof Promise)) {
            if (result instanceof Error || Array.isArray(result)) {
              failedPaths.add(path);
            } else {
              failedPaths.delete(path);
            }
          }
          return result;
        };
        return { ...rule, validator: tracked };
      });
    }
    return tracked;
  }

  function reset(): void {
    failedPaths.clear();
  }

  function failedCount(): number {
    return failedPaths.size;
  }

  function refreshVisible(formRef: Ref<FormInst | null>): void {
    if (failedPaths.size === 0) {
      return;
    }
    void formRef.value?.validate(undefined, [...failedPaths]).catch(() => {});
  }

  return { trackRules, reset, failedCount, refreshVisible };
}

export type VisibleValidation = ReturnType<typeof createVisibleValidation>;
