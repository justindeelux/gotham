# Brand glyphs

Vendored monochrome brand shapes for resource avatars (JUS-274). The glyphs
are bundled at build time; the app never requests a CDN or any other runtime
network resource for them.

- Source: [Simple Icons](https://github.com/simple-icons/simple-icons), pinned
  version `13.21.0`
- Downloaded from: `https://cdn.jsdelivr.net/npm/simple-icons@13.21.0/icons/<slug>.svg`
- License: CC0-1.0 (see `LICENSE`), no attribution required
- Files and slugs: `git.svg` (`git`), `github.svg` (`github`), `gitlab.svg`
  (`gitlab`), `docker.svg` (`docker`), `postgresql.svg` (`postgresql`),
  `mysql.svg` (`mysql`), `mariadb.svg` (`mariadb`), `mongodb.svg` (`mongodb`),
  `redis.svg` (`redis`), `wordpress.svg` (`wordpress`), `nextcloud.svg`
  (`nextcloud`), `n8n.svg` (`n8n`), `uptimekuma.svg` (`uptimekuma`)

Each file is an unmodified Simple Icons shape (24×24 viewBox, single path, no
script, no external reference). The avatar tint is applied by the UI: the
shape is masked over a white foreground inside a colored rounded frame, so a
glyph stays monochrome on every background.
