import type {
  LocationQueryValue,
  RouteLocationNormalizedLoaded,
  RouteLocationRaw,
} from "vue-router";

export function safeRedirect(
  value: LocationQueryValue | LocationQueryValue[],
): string | null {
  if (typeof value !== "string" || !value.startsWith("/")) {
    return null;
  }
  const head = value.slice(0, 2).replaceAll("\\", "/");
  return head === "//" ? null : value;
}

export function authSwitchTarget(
  route: RouteLocationNormalizedLoaded,
  name: "login" | "register",
): RouteLocationRaw {
  const redirect = safeRedirect(route.query.redirect);
  return redirect ? { name, query: { redirect } } : { name };
}
