// JUS-275: the shared floating-window frame owns the chrome and the
// drag/resize mechanics both floating tools reuse. These checks pin the
// contract the editor and terminal rely on: slots land in the frame, a drag
// never starts on interactive chrome, and the grips report shared geometry.
/* global document:readonly, Element:readonly, MouseEvent:readonly, window:readonly */
import { mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { nextTick } from "vue";

import FloatingWindow from "@/shared/ui/FloatingWindow.vue";
import { windowResizeLimits } from "@/shared/window/geometry";

const limits = windowResizeLimits(1280, 800, { side: 48, chrome: 104 }, 480, 320);

function mountFrame() {
  return mount(FloatingWindow, {
    attachTo: document.body,
    props: {
      windowLabel: "Test window",
      minimizeLabel: "Minimize",
      closeLabel: "Close",
      resizeLabel: "Resize",
      size: { width: 800, height: 600 },
      resizeLimits: limits,
    },
    slots: {
      title: "Title",
      context: "Context",
      actions:
        '<button type="button" class="slot-action">Act</button><span role="button" class="slot-role-action">Role</span>',
      tabs: '<div class="slot-tabs">Tabs</div>',
      default: '<div class="slot-body">Body</div>',
    },
  });
}

/** firePointer dispatches a pointer-ish mouse event jsdom can carry. */
function firePointer(
  target: Element,
  type: string,
  init: { button?: number; clientX?: number; clientY?: number; pointerId?: number },
): void {
  const event = new MouseEvent(type, {
    bubbles: true,
    cancelable: true,
    button: init.button ?? 0,
    clientX: init.clientX ?? 0,
    clientY: init.clientY ?? 0,
  });
  Object.defineProperty(event, "pointerId", { value: init.pointerId ?? 1 });
  target.dispatchEvent(event);
}

describe("FloatingWindow", () => {
  const pointerCapture = window.HTMLElement.prototype.setPointerCapture;

  beforeEach(() => {
    window.HTMLElement.prototype.setPointerCapture = () => {};
  });

  afterEach(() => {
    window.HTMLElement.prototype.setPointerCapture = pointerCapture;
    document.body.innerHTML = "";
  });

  it("renders the shared frame with the title, context, tabs, body and actions", () => {
    const wrapper = mountFrame();
    const frame = wrapper.find(".floating-window");

    expect(frame.exists()).toBe(true);
    expect(frame.attributes("role")).toBe("region");
    expect(frame.attributes("aria-label")).toBe("Test window");
    expect(wrapper.find(".floating-window-bar .floating-window-title").text()).toBe("Title");
    expect(wrapper.find(".floating-window-context").text()).toBe("Context");
    expect(wrapper.find(".floating-window-actions .slot-action").exists()).toBe(true);
    expect(wrapper.find(".slot-tabs").exists()).toBe(true);
    expect(wrapper.find(".floating-window-body .slot-body").text()).toBe("Body");
    wrapper.unmount();
  });

  it("offers eight grips, with only the SE corner as the keyboard slider", () => {
    const wrapper = mountFrame();
    const resizeClass = wrapper.find(".floating-window-resize--se");

    expect(wrapper.findAll(".floating-window-resize")).toHaveLength(8);
    expect(resizeClass.attributes("role")).toBe("slider");
    expect(resizeClass.attributes("tabindex")).toBe("0");
    expect(resizeClass.attributes("aria-valuenow")).toBe("800");
    for (const direction of ["n", "ne", "e", "s", "sw", "w", "nw"]) {
      expect(wrapper.find(`.floating-window-resize--${direction}`).attributes("aria-hidden")).toBe(
        "true",
      );
    }
    wrapper.unmount();
  });

  it("drags from the empty bar but never from an action button", async () => {
    const wrapper = mountFrame();
    const bar = wrapper.find(".floating-window-bar").element;

    const action = wrapper.find(".slot-action").element;
    firePointer(action, "pointerdown", { clientX: 30, clientY: 40 });
    firePointer(action, "pointermove", { clientX: 130, clientY: 140 });
    expect(wrapper.emitted("drag")).toBeUndefined();

    // A custom control that only carries role=button owns its clicks too.
    const roleAction = wrapper.find(".slot-role-action").element;
    firePointer(roleAction, "pointerdown", { clientX: 30, clientY: 40 });
    firePointer(roleAction, "pointermove", { clientX: 130, clientY: 140 });
    expect(wrapper.emitted("drag")).toBeUndefined();

    firePointer(bar, "pointerdown", { clientX: 30, clientY: 40 });
    firePointer(bar, "pointermove", { clientX: 130, clientY: 140 });
    await nextTick();
    expect(wrapper.emitted("drag")).toEqual([[100, 100]]);
    wrapper.unmount();
  });

  it("reports grip resizes and keyboard steps from the displayed size", async () => {
    const wrapper = mountFrame();
    const grip = wrapper.find(".floating-window-resize--se").element;

    firePointer(grip, "pointerdown", { clientX: 200, clientY: 200 });
    firePointer(grip, "pointermove", { clientX: 216, clientY: 224 });
    await nextTick();
    expect(wrapper.emitted("resize")).toEqual([
      [{ x: 0, y: 0, width: 816, height: 624 }],
    ]);

    await wrapper.find(".floating-window-resize--se").trigger("keydown", {
      key: "ArrowLeft",
    });
    expect(wrapper.emitted("resizeBy")).toEqual([[-16, 0]]);
    wrapper.unmount();
  });

  it("emits minimize and close from the shared controls", async () => {
    const wrapper = mountFrame();

    await wrapper.find('[aria-label="Minimize"]').trigger("click");
    await wrapper.find('[aria-label="Close"]').trigger("click");

    expect(wrapper.emitted("minimize")).toHaveLength(1);
    expect(wrapper.emitted("close")).toHaveLength(1);
    wrapper.unmount();
  });
});

describe("shared window geometry", () => {
  it("keeps the terminal and editor resize bands identical", () => {
    expect(windowResizeLimits(1280, 800, { side: 48, chrome: 104 }, 460, 300)).toEqual({
      minWidth: 460,
      minHeight: 300,
      maxWidth: 1232,
      maxHeight: 696,
      viewportWidth: 1280,
      viewportHeight: 800,
    });
  });
});
