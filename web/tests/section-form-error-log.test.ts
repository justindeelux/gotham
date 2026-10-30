import { NMessageProvider } from "naive-ui";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import { useSectionForm } from "@/shared/composables/useSectionForm";

function mountForm(save: () => Promise<unknown>) {
  let form: ReturnType<typeof useSectionForm<unknown>> | null = null;
  const Child = defineComponent({
    setup() {
      form = useSectionForm(save, () => {}, () => "saved");
      return () => h("div");
    },
  });
  const wrapper = mount(
    defineComponent({
      render() {
        return h(NMessageProvider, null, { default: () => h(Child) });
      },
    }),
  );
  return { wrapper, form: form as unknown as ReturnType<typeof useSectionForm<unknown>> };
}

describe("useSectionForm error display", () => {
  it("shows the helper's reason from the response log with the message", async () => {
    const save = vi.fn().mockRejectedValue({
      status: 502,
      message: "the host helper failed; the change was not applied",
      cause: {
        response: {
          data: {
            log: "apply-system: mkdir: cannot create directory '/etc/systemd/timesyncd.conf.d': Read-only file system",
          },
        },
      },
    });
    const { wrapper, form } = mountForm(save);

    await form.submit();
    await flushPromises();

    expect(form.errorMessage.value).toContain("the host helper failed; the change was not applied");
    expect(form.errorMessage.value).toContain(
      "apply-system: mkdir: cannot create directory '/etc/systemd/timesyncd.conf.d': Read-only file system",
    );
    wrapper.unmount();
  });
});
