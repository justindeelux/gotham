// Form feedback styling contract (JUS-16/17/18), component half.
//
// Computed-style behaviour lives in the Playwright suite
// (form-feedback.spec.ts, run via npm run test:css): jsdom never applies
// stylesheets, so this suite only asserts what jsdom can prove — the real
// Naive UI DOM provides the structural hooks the global rules rely on
// (:empty wrapper when valid, .n-form-item-blank--error when in error,
// single-line feedback swap in DynamicForm), plus the theme token value.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { mount } from "@vue/test-utils";
import { NFormItem, NInput } from "naive-ui";
import { describe, expect, it } from "vitest";
import { h } from "vue";

import DynamicForm from "../src/components/DynamicForm.vue";
import type { TemplateField } from "../src/api/templates";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const mainCss = readFileSync(resolve(webRoot, "src/styles/main.css"), "utf8");
const appVue = readFileSync(resolve(webRoot, "src/App.vue"), "utf8");

const fields: TemplateField[] = [
  {
    key: "token",
    label: "Token",
    type: "text",
    required: false,
    help: "Paste the provider token.",
  },
];

describe("JUS-16 error text size token", () => {
  it("sets the 12px feedback size once in the Naive theme overrides", () => {
    for (const size of ["Small", "Medium", "Large"]) {
      expect(appVue).toContain(`feedbackFontSize${size}: "12px"`);
    }
  });

  it("does not fight the theme with a competing CSS font-size", () => {
    expect(mainCss).not.toMatch(/\.n-form-item-feedback\s*\{[^}]*font-size/);
  });
});

describe("JUS-17 no reserved feedback height when valid", () => {
  it("renders a valid field with an empty feedback wrapper", () => {
    const wrapper = mount(NFormItem, {
      props: { label: "Node name" },
      slots: { default: () => h(NInput, { value: "" }) },
      // Disable the test-utils Transition stub: in real browsers the
      // Transition renders nothing here, leaving only a comment node.
      global: { stubs: { transition: false } },
    });
    const feedback = wrapper.find(".n-form-item-feedback-wrapper");
    expect(feedback.exists()).toBe(true);
    // No element children and no text: matches :empty in real browsers, so
    // the global collapse rule applies and no blank line renders.
    expect(feedback.element.children.length).toBe(0);
    expect(feedback.element.textContent).toBe("");
  });
});

describe("JUS-18 hint hidden while error shown", () => {
  it("marks the input row in error while keeping the hint in the DOM", () => {
    const wrapper = mount(NFormItem, {
      props: {
        label: "Node name",
        feedback: "Enter a node name.",
        validationStatus: "error",
      },
      slots: {
        default: () => [
          h(NInput, { value: "" }),
          h("span", { class: "field-hint" }, "A short unique name."),
        ],
      },
    });
    // The hook the global hide rule keys on; the error line stays in the
    // accessibility tree next to the input (not color-only, still
    // associated), while the hint returns as soon as the status clears.
    expect(
      wrapper.find(".n-form-item-blank--error").exists(),
    ).toBe(true);
    expect(wrapper.find(".field-hint").exists()).toBe(true);
    expect(
      wrapper.find(".n-form-item-feedback--error").exists(),
    ).toBe(true);
  });

  it("swaps help for error in DynamicForm's single feedback line", () => {
    const valid = mount(DynamicForm, {
      props: { fields, modelValue: {}, errors: {} },
    });
    expect(valid.find(".n-form-item-feedback").text()).toContain(
      "Paste the provider token.",
    );
    expect(
      valid.find(".n-form-item-blank--error").exists(),
    ).toBe(false);

    const invalid = mount(DynamicForm, {
      props: { fields, modelValue: {}, errors: { token: "Token is required." } },
    });
    const feedback = invalid.find(".n-form-item-feedback");
    expect(feedback.text()).toContain("Token is required.");
    expect(feedback.text()).not.toContain("Paste the provider token.");
    expect(
      invalid.find(".n-form-item-blank--error").exists(),
    ).toBe(true);
  });
});
