import type { FormInst, FormItemRule, FormRules } from "naive-ui";
import type { Ref } from "vue";

/** RuleValidator is one Naive form-item validator. */
type RuleValidator = NonNullable<FormItemRule["validator"]>;

/**
 * Visible-feedback tracking for locale switches (I18N-2 fix round).
 *
 * Naive rules fire on input/blur/submit, so errors are routinely visible
 * before any submit; revalidating the whole form on a language switch would
 * surface errors on pristine fields, while a submit flag misses pre-submit
 * feedback. Instead each rule validator is wrapped to record whether its
 * path last failed: a locale switch then revalidates exactly the paths with
 * visible feedback. Drafts, triggers, required marks and submit behavior are
 * untouched, nothing submits, and no API is called. Clearing the record on
 * success keeps a reset form pristine across later switches.
 */
export function createVisibleValidation() {
  /** failedPaths holds paths whose latest validation run failed. */
  const failedPaths = new Set<string>();

  /** trackRules wraps every rule validator to record per-path feedback. */
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
          // Sync validators only (every ruleFrom adapter in scope is
          // sync); an async result leaves the record unchanged.
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

  /** reset clears the record (success paths) so reset forms stay pristine. */
  function reset(): void {
    failedPaths.clear();
  }

  /** failedCount reports tracked paths (regression tests). */
  function failedCount(): number {
    return failedPaths.size;
  }

  /**
   * refreshVisible revalidates only paths with visible feedback. No-op when
   * nothing is showing, so pristine forms never gain errors from a switch.
   */
  function refreshVisible(formRef: Ref<FormInst | null>): void {
    if (failedPaths.size === 0) {
      return;
    }
    void formRef.value?.validate(undefined, [...failedPaths]).catch(() => {});
  }

  return { trackRules, reset, failedCount, refreshVisible };
}

/** VisibleValidation is the tracker handle created per form mount. */
export type VisibleValidation = ReturnType<typeof createVisibleValidation>;
