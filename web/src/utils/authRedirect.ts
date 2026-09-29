import type {
  LocationQueryValue,
  RouteLocationNormalizedLoaded,
  RouteLocationRaw,
} from "vue-router";

/**
 * Auth redirects.
 *
 * The router guard sends an unauthenticated visitor to
 * `/login?redirect=<fullPath>`, and both auth pages honour that query after a
 * successful sign-in or registration. The same query has to survive the switch
 * between the two pages: a first-time invite recipient who lands on the sign-in
 * form, opens "Create account" and registers must still end up accepting the
 * invitation instead of being dropped on the dashboard.
 *
 * The token itself stays in the URL query only — it is never written to
 * localStorage or sessionStorage.
 */

/**
 * safeRedirect reads a `?redirect` value, returning it only when it is a local
 * absolute path. Anything else (a missing value, a repeated parameter, a
 * protocol-relative `//host` or an absolute URL) is refused, so an auth
 * redirect can never become an open redirect.
 */
export function safeRedirect(
  value: LocationQueryValue | LocationQueryValue[],
): string | null {
  if (typeof value !== "string") {
    return null;
  }
  return value.startsWith("/") && !value.startsWith("//") ? value : null;
}

/**
 * authSwitchTarget links between the sign-in and registration pages, carrying
 * a safe `?redirect` across the switch.
 */
export function authSwitchTarget(
  route: RouteLocationNormalizedLoaded,
  name: "login" | "register",
): RouteLocationRaw {
  const redirect = safeRedirect(route.query.redirect);
  return redirect ? { name, query: { redirect } } : { name };
}
