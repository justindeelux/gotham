/**
 * Canonical nested-URL guards (PE-5 fix round 1, H1): a resource id under a
 * wrong project/environment URL must not render silently. After the fetch,
 * the response's ids are the authority — when they differ from the route,
 * the caller replaces the URL with the canonical one; when the response is
 * a 404 the caller renders its not-found state instead.
 */

/** RouteScope is the project/environment pair a nested URL claims. */
export interface RouteScope {
  projectId: string;
  environmentId: string;
}

/**
 * resolveEnvironmentScope compares the resources response with the route.
 * Null when the URL is canonical; otherwise the scope the URL must be
 * replaced with (taken from the response, never from the typed params).
 */
export function resolveEnvironmentScope(
  response: { projectId: string; environmentId: string },
  route: RouteScope,
): RouteScope | null {
  if (
    response.projectId === route.projectId &&
    response.environmentId === route.environmentId
  ) {
    return null;
  }
  return { projectId: response.projectId, environmentId: response.environmentId };
}
