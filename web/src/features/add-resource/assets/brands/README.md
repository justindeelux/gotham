# Brand glyphs

Vendored monochrome brand shapes for application sources (JUS-274). The glyphs
are bundled at build time; the app never requests a CDN or any other runtime
network resource for them. Service and database icons live in `../icons` and
use their own full-colour assets.

- Source: [Simple Icons](https://github.com/simple-icons/simple-icons), pinned
  version `13.21.0`
- Downloaded from: `https://cdn.jsdelivr.net/npm/simple-icons@13.21.0/icons/<slug>.svg`
- License: CC0-1.0 (see `LICENSE`), no attribution required
- Files and slugs: `git.svg` (`git`), `github.svg` (`github`), `gitlab.svg`
  (`gitlab`), `docker.svg` (`docker`)

Each file is an unmodified Simple Icons shape (24×24 viewBox, single path, no
script, no external reference). The avatar tint is applied by the UI: the
shape is masked over a white foreground inside a coloured rounded frame, so a
glyph stays monochrome on every background.
