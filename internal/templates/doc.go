// Package templates is the Phase 7 (BE-7.2) one-click template engine: it
// loads the built-in catalog (the repository's templates/ directory, embedded
// into the binary), validates every template definition, and renders a
// compose document from form-field values.
//
// # Template format
//
// One template is one directory, templates/{slug}/:
//
//   - template.yaml — metadata (name, icon, description) and the form fields,
//     each with a type, a required flag, an optional default and validation.
//   - compose.yaml — the compose document with {{ .field }} placeholders.
//
// The slug is the directory name and must match ^[a-z][a-z0-9-]{0,63}$.
// Supported field types are text, secret, number, select and bool; see Field
// and the README next to the built-in templates for the complete schema.
//
// # Rendering contract
//
// Rendering is strict, non-executing string substitution. Only the exact
// placeholder form {{ .field }} is accepted: no functions, no
// template/define/block, no conditionals and no nesting. A scalar that does
// not contain "{{" is copied verbatim; an unknown field, a malformed
// placeholder, a missing required value, an extra value, a wrong type or a
// value failing its validation is an error. Each substituted value has every
// literal dollar doubled ($ -> $$) so the result survives the service
// pipeline's own ${VAR} interpolation untouched, and the YAML tree is
// re-encoded, so a value can never inject YAML structure. Rendering is
// deterministic: the same values produce the same bytes.
//
// The rendered document is validated with services.Parse before it is
// returned, so it always satisfies the compose subset the deploy path
// (BE-7.1) accepts: image-only services, declared named volumes and the
// gotham.domain label convention for routing. ${VAR} references left in a
// template are interpolated by the services pipeline at create/deploy time,
// exactly as for a hand-written compose document.
//
// # HTTP surface
//
// The package also mounts the authenticated catalog routes (see Mount):
// GET /api/v1/templates, GET /api/v1/templates/{slug} and
// POST /api/v1/templates/{slug}/render. The surface renders and shows
// documents only; it never deploys. FE-7.1 renders a template, then creates
// and deploys a service through the existing services endpoints.
//
// FEATURE_SERVICES=false disables the whole surface: without the service
// deploy path a rendered document has nowhere to go.
package templates
