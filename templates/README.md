# One-click service templates

A template renders a small form into a compose document, which deploys through
the Phase 7 service path (BE-7.1). The built-in catalog is embedded in the
control-plane binary: adding or editing a template changes the binary, and the
control plane validates every template when it loads the catalog.

## Layout

```
templates/{slug}/
  template.yaml   # metadata + form fields
  compose.yaml    # the compose document with {{ .field }} placeholders
```

`{slug}` is the template's identifier: lowercase letters, digits and `-`,
starting with a letter (for example `uptime-kuma`). `templates/README.md`
itself is not embedded and is not a template.

## template.yaml

```yaml
name: WordPress                 # display name, 1-80 characters
icon: wordpress                 # icon key the gallery resolves, [a-z0-9-]
description: WordPress with a MySQL database and persistent volumes.
fields:                         # 1-32 fields
  - key: domain                 # [a-z][a-z0-9_]*; referenced as {{ .domain }}
    label: Domain               # optional, defaults to the key
    type: text                  # text | secret | number | select | bool
    required: true
    placeholder: blog.example.com
    help: The public host served by the node's Traefik proxy.
    pattern: ^[A-Za-z0-9][A-Za-z0-9.-]*$   # text/secret only, full-value match
    max_length: 253                        # text/secret only, characters
  - key: count
    type: number
    default: 2
    min: 1                       # number only
    max: 10
  - key: mode
    type: select
    default: fast
    options: [fast, slow]        # select only
  - key: debug
    type: bool
    default: false
  - key: db_password
    type: secret
    required: true
```

Unknown keys are rejected, so a typo never drops configuration silently.

Rules the loader enforces:

- `name`, `icon`, `description` and at least one field are required.
- A field that is not `required` must declare a `default`; every default must
  itself pass the field's type rules (so `default: 20` with `max: 10` fails at
  load).
- `secret` fields must be `required` and must not declare a `default`: a
  password is never baked into a template.
- `pattern`/`max_length` apply to `text` and `secret`, `min`/`max` to
  `number`, `options` to `select`; anything else is an error.
- Every declared field must be referenced by `compose.yaml` at least once, and
  every placeholder must name a declared field.

At render time the caller supplies values as JSON scalars (string, whole
number or boolean). Missing required values, unknown fields, wrong types and
values failing `pattern`, `max_length`, `min`/`max` or `options` are rejected
with a clear error.

## compose.yaml

The document uses `{{ .field }}` placeholders and is otherwise a normal
compose document restricted to the BE-7.1 subset: image-only services, named
volumes, no `build`/`extends`/`env_file`/`include`. A public host is declared
with the Gotham label convention and the container port must be published for
Traefik to reach it:

```yaml
services:
  app:
    image: nginx:1.27
    labels:
      gotham.domain: "{{ .domain }}"
      gotham.domain.port: "80"
    ports:
      - "80"
volumes:
  app_data:
```

Rendering is strict, non-executing string substitution:

- The only supported syntax is `{{ .field }}` (whitespace around the field
  name is allowed). Template actions, pipes, functions, conditionals,
  `define`/`template`/`block`, comments and `{{-`/`-}}` are load errors: a
  template can never execute anything.
- There are no conditionals, so a template is a fixed topology. An "optional"
  service cannot be omitted at render time; ship it in the stack and let the
  operator edit the rendered service afterwards.
- The compose YAML is re-encoded after substitution, so a value can never
  inject YAML structure. Comments in `compose.yaml` are not preserved by the
  re-encode.
- Every literal `$` in a substituted value is doubled (`$` -> `$$`) so the
  value survives the service pipeline's own `${VAR}` interpolation untouched
  and reaches the container literally. `${VAR}` references left in
  `compose.yaml` outside a placeholder are interpolated by the services
  pipeline at create/deploy time, exactly as for a hand-written document.
- Rendering is deterministic: the same values produce the same bytes.

## HTTP contract (FE-7.1)

All routes require authentication with the admin scope, like the services
routes they feed. `FEATURE_SERVICES=false` disables them together with the
services surface.

`GET /api/v1/templates` — the catalog, metadata only:

```json
{ "templates": [ { "slug": "wordpress", "name": "WordPress", "icon": "wordpress", "description": "..." } ] }
```

`GET /api/v1/templates/{slug}` — metadata plus the form schema:

```json
{ "template": { "slug": "wordpress", "name": "WordPress", "icon": "wordpress",
  "description": "...", "fields": [ { "key": "domain", "label": "Domain", "type": "text",
  "required": true, "placeholder": "blog.example.com", "pattern": "...", "max_length": 253 } ] } }
```

A `secret` field never carries a `default`. `number` fields carry `min`/`max`,
`select` fields carry `options`.

`POST /api/v1/templates/{slug}/render` — render with field values:

```json
{ "values": { "domain": "blog.example.com", "db_password": "..." } }
```

```json
{ "slug": "wordpress",
  "compose_yaml": "services:\n  ...",
  "spec": { "services": ["mysql", "wordpress"],
            "domains": [ { "service": "wordpress", "domain": "blog.example.com", "port": 80 } ],
            "named_volumes": ["mysql_data", "wordpress_data"],
            "mounts": [ { "service": "wordpress", "source": "wordpress_data", "target": "/var/www/html", "named": true } ] } }
```

Validation failures answer `400` with `{"message": "..."}`; an unknown slug
answers `404`. The gallery flow is: render, then create a service with
`compose_yaml` (and no env) through `POST /api/v1/services`, then deploy it
through `POST /api/v1/services/{id}/deploy`. The rendered document has already
passed the services schema check, so the create cannot fail on the document.

## Adding a template

1. Create `templates/{slug}/template.yaml` and `compose.yaml`.
2. Run `go test ./internal/templates/...`: the built-in catalog test validates
   every shipped template. With Docker available, run the gated acceptance
   `GOTHAM_E2E=1 go test ./internal/e2e/ -run 'TestP7Template' -count=1 -v`,
   which renders each template, validates it with the node's own
   `docker compose config` and deploys WordPress end to end.
3. Rebuild the control plane: the files are embedded with `go:embed`.
