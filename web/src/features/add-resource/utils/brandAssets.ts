export interface BrandAsset {
  url: string;
  tinted: boolean;
}

type Loader = () => Promise<string>;

// Lazy loaders keep the icons out of the main chunk; the gallery has hundreds.
const iconLoaders = import.meta.glob<string>(
  "../assets/icons/**/*.{svg,png,webp,jpeg,jpg,ico}",
  { query: "?url", import: "default" },
);
const glyphLoaders = import.meta.glob<string>("../assets/brands/*.svg", {
  query: "?url",
  import: "default",
});

function keyed(loaders: Record<string, Loader>): Map<string, Loader> {
  return new Map(
    Object.entries(loaders).map(([path, load]) => [
      path.slice(path.lastIndexOf("/") + 1).replace(/\.[^.]+$/, ""),
      load,
    ]),
  );
}

export function createAssetResolver(
  icons: Map<string, Loader>,
  glyphs: Map<string, Loader>,
) {
  // Source tiles and catalog slugs can share a name, so the folder is part of the key.
  const cache = new Map<string, Promise<BrandAsset>>();

  async function fallback(): Promise<BrandAsset> {
    const load = icons.get("default");
    return { url: load === undefined ? "" : await load(), tinted: false };
  }

  async function loadAsset(logo: string, glyph: boolean): Promise<BrandAsset> {
    const source = glyph ? glyphs.get(logo) : undefined;
    if (source !== undefined) {
      return { url: await source(), tinted: true };
    }
    const icon = icons.get(logo);
    return icon === undefined
      ? await fallback()
      : { url: await icon(), tinted: false };
  }

  return {
    async assetFor(logo: string, glyph: boolean): Promise<BrandAsset> {
      const key = `${glyph ? "glyph" : "icon"}:${logo}`;
      let pending = cache.get(key);
      if (pending === undefined) {
        pending = loadAsset(logo, glyph).catch(async () => {
          // A chunk that failed to load must not stay cached across a self-update.
          cache.delete(key);
          return await fallback();
        });
        cache.set(key, pending);
      }
      return await pending;
    },
  };
}

export const { assetFor } = createAssetResolver(
  keyed(iconLoaders),
  keyed(glyphLoaders),
);
