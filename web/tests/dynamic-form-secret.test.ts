import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

import type { TemplateField, TemplateValues } from "@/features/templates/api/templates";
import DynamicForm from "@/features/templates/components/DynamicForm.vue";
import { registerDiscoveredCatalogs, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const fields: TemplateField[] = [
  {
    key: "db_password",
    label: "Database password",
    type: "secret",
    required: true,
    generate: true,
  },
  { key: "root_password", label: "Root password", type: "secret", required: true },
  { key: "domain", label: "Domain", type: "text", required: true },
];

const writeText = vi.fn<() => Promise<void>>().mockResolvedValue(undefined);

function mountForm(values: TemplateValues, formFields: TemplateField[] = fields) {
  return mount(
    defineComponent({
      setup() {
        const modelValue = ref<TemplateValues>({ ...values });
        return () =>
          h(NMessageProvider, null, {
            default: () =>
              h(DynamicForm, {
                fields: formFields,
                modelValue: modelValue.value,
                "onUpdate:modelValue": (next: TemplateValues) => {
                  modelValue.value = next;
                },
              }),
          });
      },
    }),
    { attachTo: globalThis.document.body },
  );
}

function valueOf(wrapper: ReturnType<typeof mountForm>, key: string): string {
  const input = wrapper.find(`.field-${key} input`).element as unknown as { value: string };
  return input.value;
}

beforeEach(() => {
  resetLocaleState();
  registerDiscoveredCatalogs();
  syncComposerLocale("en");
  writeText.mockClear();
  Object.defineProperty(globalThis.navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });
});

describe("DynamicForm secret generation", () => {
  it("shows Generate only on generatable secret fields", () => {
    const wrapper = mountForm({ db_password: "", root_password: "", domain: "" });
    expect(wrapper.find('[aria-label="Generate Database password"]').exists()).toBe(true);
    expect(wrapper.find('[aria-label="Generate Root password"]').exists()).toBe(false);
    expect(wrapper.find('[aria-label="Generate Domain"]').exists()).toBe(false);
    expect(wrapper.find('[aria-label="Copy Database password"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("fills a 24 character value, reveals it and offers Copy", async () => {
    const wrapper = mountForm({ db_password: "", root_password: "", domain: "" });
    await wrapper.find('[aria-label="Generate Database password"]').trigger("click");
    await flushPromises();

    expect(valueOf(wrapper, "db_password")).toMatch(/^[A-Za-z0-9]{24}$/);
    expect(wrapper.find(".field-db_password input").attributes("type")).toBe("text");
    expect(wrapper.find('[aria-label="Copy Database password"]').exists()).toBe(true);
    expect(valueOf(wrapper, "root_password")).toBe("");
    wrapper.unmount();
  });

  it("produces a different value on every click and keeps a typed value", async () => {
    const wrapper = mountForm({ db_password: "", root_password: "", domain: "" });
    const generate = wrapper.find('[aria-label="Generate Database password"]');
    await generate.trigger("click");
    await flushPromises();
    const first = valueOf(wrapper, "db_password");
    await generate.trigger("click");
    await flushPromises();
    expect(valueOf(wrapper, "db_password")).not.toBe(first);

    const input = wrapper.find(".field-db_password input");
    await input.setValue("manual-secret");
    expect(valueOf(wrapper, "db_password")).toBe("manual-secret");
    wrapper.unmount();
  });

  it("copies the value and announces success", async () => {
    const wrapper = mountForm({ db_password: "", root_password: "", domain: "" });
    await wrapper.find('[aria-label="Generate Database password"]').trigger("click");
    await flushPromises();
    const generated = valueOf(wrapper, "db_password");

    await wrapper.find('[aria-label="Copy Database password"]').trigger("click");
    await flushPromises();

    expect(writeText).toHaveBeenCalledWith(generated);
    expect(globalThis.document.body.textContent).toContain("Database password copied to clipboard.");
    wrapper.unmount();
  });

  it("announces generation and copy in the live region without the value", async () => {
    const wrapper = mountForm({ db_password: "", root_password: "", domain: "" });
    const live = wrapper.find('.dynamic-form [role="status"]');
    expect(live.text()).toBe("");

    await wrapper.find('[aria-label="Generate Database password"]').trigger("click");
    await flushPromises();
    expect(live.text()).toBe("Database password generated.");
    expect(live.text()).not.toContain(valueOf(wrapper, "db_password"));

    await wrapper.find('[aria-label="Copy Database password"]').trigger("click");
    await flushPromises();
    expect(live.text()).toBe("Copied.");
    wrapper.unmount();
  });

  it("caps a generated value at the field's max_length", async () => {
    const wrapper = mountForm({ short: "" }, [
      {
        key: "short",
        label: "Short secret",
        type: "secret",
        required: true,
        generate: true,
        max_length: 16,
      },
    ]);
    await wrapper.find('[aria-label="Generate Short secret"]').trigger("click");
    await flushPromises();
    expect(valueOf(wrapper, "short")).toMatch(/^[A-Za-z0-9]{16}$/);
    wrapper.unmount();
  });
});
