import { inject, nextTick, onBeforeUnmount, onMounted, provide, ref, watch } from "vue";
import type { InjectionKey, Ref } from "vue";
import { useRoute } from "vue-router";

export interface MobileNav {
  mobileNavOpen: Ref<boolean>;
  sidebarRef: Ref<HTMLElement | null>;
  toggleNav: () => void;
  closeMobileNav: () => void;
}

const mobileNavKey: InjectionKey<MobileNav> = Symbol("mobileNav");

function focusables(root: HTMLElement): HTMLElement[] {
  const selector =
    'a[href], button:not([disabled]), input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])';
  return Array.from(root.querySelectorAll<HTMLElement>(selector)).filter(
    (element) => element.offsetParent !== null,
  );
}

export function provideMobileNav(): MobileNav {
  const route = useRoute();

  const mobileNavOpen = ref(false);
  const sidebarRef = ref<HTMLElement | null>(null);

  const mobileQuery = window.matchMedia("(max-width: 1024px)");

  let restoreFocus: HTMLElement | null = null;

  function onDrawerKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      event.preventDefault();
      closeMobileNav();
      return;
    }
    if (event.key !== "Tab") {
      return;
    }
    const root = sidebarRef.value;
    if (!root) {
      return;
    }
    const items = focusables(root);
    if (items.length === 0) {
      return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement;
    if (event.shiftKey && (active === first || !root.contains(active))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  function toggleNav(): void {
    if (mobileNavOpen.value) {
      closeMobileNav();
      return;
    }
    restoreFocus =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    mobileNavOpen.value = true;
  }

  function closeMobileNav(): void {
    mobileNavOpen.value = false;
  }

  function onViewportChange(event: MediaQueryListEvent): void {
    if (!event.matches) {
      closeMobileNav();
    }
  }

  watch(mobileNavOpen, async (open) => {
    if (open) {
      window.addEventListener("keydown", onDrawerKeydown);
      await nextTick();
      const root = sidebarRef.value;
      if (root) {
        (focusables(root)[0] ?? root).focus();
      }
      return;
    }
    window.removeEventListener("keydown", onDrawerKeydown);
    restoreFocus?.focus();
    restoreFocus = null;
  });

  watch(
    () => route.path,
    () => {
      mobileNavOpen.value = false;
    },
  );

  onMounted(() => {
    mobileQuery.addEventListener("change", onViewportChange);
  });

  onBeforeUnmount(() => {
    mobileQuery.removeEventListener("change", onViewportChange);
    window.removeEventListener("keydown", onDrawerKeydown);
  });

  const nav: MobileNav = { mobileNavOpen, sidebarRef, toggleNav, closeMobileNav };
  provide(mobileNavKey, nav);
  return nav;
}

export function useMobileNav(): MobileNav {
  const nav = inject(mobileNavKey);
  if (!nav) {
    throw new Error("useMobileNav must be used inside AppLayout (missing mobileNav provider)");
  }
  return nav;
}
