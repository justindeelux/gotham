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
// A secret field is never written into the document: it renders as a ${field}
// reference and its value is returned in RenderResult.Env, so the caller can
// pass it to services.Create unchanged. That reuses the BE-7.1 environment
// contract: the services pipeline substitutes the secret at deploy time and
// redacts it from every error, log line and deploy-history row. Every error
// Render itself returns is additionally passed through services.RedactError
// with the supplied secret values (raw and dollar-escaped forms), so a secret
// can never be echoed even by a future parse-visible path.
//
// The rendered document is validated with services.Parse before it is
// returned, so it always satisfies the compose subset the deploy path
// (BE-7.1) accepts: image-only services, declared named volumes and the
// gotham.domain label convention for routing. ${VAR} references left in a
// template are interpolated by the services pipeline at create/deploy time,
// exactly as for a hand-written compose document. The loader additionally
// validates the value-independent structural subset (image-only services, no
// build/extends/env_file/include) so a template that cannot deploy is
// rejected before it enters the gallery.
//
// # HTTP surface
//
// The package also mounts the authenticated catalog routes (see Mount):
// GET /api/v1/templates, GET /api/v1/templates/{slug} and
// POST /api/v1/templates/{slug}/render. The surface renders and shows
// documents only; it never deploys. FE-7.1 renders a template, then creates
// and deploys a service through the existing services endpoints, passing the
// render response's env through unchanged.
//
// FEATURE_SERVICES=false disables the whole surface: without the service
// deploy path a rendered document has nowhere to go. Set before startup the
// routes are not mounted (404); set while running, every call answers 503,
// matching the service operations.
package templates
