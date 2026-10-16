/**
 * Not-found English catalog (namespace `not-found`): the standalone 404 page
 * shown for an unknown URL (JUS-193). Distinct from the permission page.
 */
const en = {
  title: "Page not found",
  description:
    "We couldn't find that page. It may have been moved or may never have existed.",
  back: "Back to dashboard",
};

export default en;

/** NotFoundMessages is the shape the Vietnamese catalog must mirror. */
export type NotFoundMessages = typeof en;
