// Go-to-line popover (JUS-231): rejects zero, out-of-range and non-numeric
// input inline, jumps on Enter for `line` and `line:column`, returns focus to
// the editor on Escape, and shows the current line and the total.
/* global document:readonly, KeyboardEvent:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import EditorGotoLine from "@/features/files/components/EditorGotoLine.vue";
// Side-effect import: merges the lazy editor catalogs under `files.editor`.
import "@/features/files/locales/editorMessages";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  document.body.innerHTML = "";
});

/** mountGoto mounts the popover open over a 10-line document. */
function mountGoto(jump = vi.fn(() => null)) {
  const wrapper = mount(EditorGotoLine, {
    attachTo: document.body,
    props: { show: true, current: 3, total: 10, jump },
    global: { plugins: [i18n] },
  });
  return { wrapper, jump };
}

/** errorText reads the inline message. */
function errorText(wrapper: ReturnType<typeof mountGoto>["wrapper"]): string {
  return wrapper.find('[role="alert"]').text();
}

describe("EditorGotoLine", () => {
  it("shows the cursor line, the total and focuses the field", async () => {
    const { wrapper } = mountGoto();
    await flushPromises();

    expect(wrapper.text()).toContain("Line 3 of 10");
    expect(document.activeElement).toBe(wrapper.find("input").element);
  });

  it("rejects zero and out-of-range lines with a message", async () => {
    const { wrapper, jump } = mountGoto();
    const input = wrapper.find("input");

    await input.setValue("0");
    await input.trigger("keydown", { key: "Enter" });
    expect(errorText(wrapper)).toBe("Enter a line from 1 to 10");
    expect(jump).not.toHaveBeenCalled();

    await input.setValue("11");
    await input.trigger("keydown", { key: "Enter" });
    expect(errorText(wrapper)).toBe("Enter a line from 1 to 10");
    expect(jump).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("rejects non-numeric input", async () => {
    const { wrapper, jump } = mountGoto();
    const input = wrapper.find("input");

    await input.setValue("abc");
    await input.trigger("keydown", { key: "Enter" });

    expect(errorText(wrapper)).toBe("Enter a line number, or line:column");
    expect(jump).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("jumps to line:column on Enter and closes", async () => {
    const { wrapper, jump } = mountGoto();
    const input = wrapper.find("input");

    await input.setValue("4:2");
    await input.trigger("keydown", { key: "Enter" });
    await flushPromises();

    expect(jump).toHaveBeenCalledWith(4, 2);
    expect(wrapper.emitted("close")).toHaveLength(1);
    wrapper.unmount();
  });

  it("shows a column rejection returned by the editor", async () => {
    const { wrapper } = mountGoto(vi.fn(() => "column"));
    const input = wrapper.find("input");

    await input.setValue("4:999");
    await input.trigger("keydown", { key: "Enter" });

    expect(errorText(wrapper)).toBe("Enter a column within the line");
    wrapper.unmount();
  });

  it("closes on Escape without letting the window see the key", async () => {
    const windowKeydown = vi.fn();
    document.addEventListener("keydown", windowKeydown);
    const { wrapper, jump } = mountGoto();
    const event = new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true,
      cancelable: true,
    });

    wrapper.find("input").element.dispatchEvent(event);
    await flushPromises();

    expect(wrapper.emitted("close")).toHaveLength(1);
    expect(jump).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(true);
    expect(windowKeydown).not.toHaveBeenCalled();
    document.removeEventListener("keydown", windowKeydown);
    wrapper.unmount();
  });
});
