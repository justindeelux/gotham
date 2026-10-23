
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { mount } from "@vue/test-utils";
import { NFormItem, NInput } from "naive-ui";
import { describe, expect, it } from "vitest";
import { h } from "vue";

import DynamicForm from "../src/features/templates/components/DynamicForm.vue";
import type { TemplateField } from "../src/features/templates/api/templates";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const mainCss = readFileSync(resolve(webRoot, "src/shared/styles/main.css"), "utf8");
const appVue = readFileSync(resolve(webRoot, "src/app/App.vue"), "utf8");

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
      global: { stubs: { transition: false } },
    });
    const feedback = wrapper.find(".n-form-item-feedback-wrapper");
    expect(feedback.exists()).toBe(true);
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
