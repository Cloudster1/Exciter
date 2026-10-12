import { useMemo, useRef, useState } from "react";
import { useDeleteThemeAsset, useUploadThemeAsset, type Theme, type ThemeAsset, type ThemeSpec } from "@/api/themes";
import { Button, Card, ErrorBanner, Field, IconButton, Segmented, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { KILOBYTE, THEME_ASSET_MAX_BYTES, THEME_CSS_MAX_BYTES, THEME_CSS_ROWS, THEME_ICON_PREVIEW_PX } from "@/config";
import { t } from "@/i18n";
import { AssetSelect, isImage, type Patch } from "./editorHelpers";

// The editor's tabs for the icons, the backdrop, the theme's files and the extra CSS.

type Effect = "none" | "constellation" | "confetti";

const UPLOAD_ACCEPT = "image/png,image/jpeg,image/webp,image/gif,image/svg+xml,.svg,.woff,.woff2,font/woff,font/woff2";

/** The glyphs the kit draws, by the name a theme keys them on. */
const glyphs: Array<{ name: string; Glyph: (typeof Icon)[keyof typeof Icon] }> = Object.values(Icon)
  .map((Glyph) => ({ name: Glyph.glyph, Glyph }))
  .sort((a, b) => a.name.localeCompare(b.name));

export function IconsTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  const [query, setQuery] = useState("");
  const shown = useMemo(() => glyphs.filter((g) => g.name.includes(query.trim().toLowerCase())), [query]);
  return (
    <Card className="p-4">
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <Field label={t.themes.editor.findIcon} value={query} onChange={(ev) => setQuery(ev.target.value)} className="w-56" />
        <p className="pb-2 text-sm text-ink-muted">{t.themes.editor.iconsIntro}</p>
      </div>
      <div className="divide-y divide-border">
        {shown.map(({ name, Glyph }) => {
          const icon = spec.icons[name];
          return (
            <div key={name} className="flex flex-wrap items-start gap-3 py-2" data-theme-icon={name}>
              <span className="flex w-40 items-center gap-2 pt-2 text-sm text-ink">
                <Glyph size={THEME_ICON_PREVIEW_PX} />
                {name}
              </span>
              <span className="space-y-1">
                <span className="block text-sm font-medium text-ink-muted">{t.themes.editor.picture}</span>
                <AssetSelect
                  label={t.themes.editor.iconPicture(name)}
                  assets={images}
                  value={icon?.assetId}
                  onChange={(id) =>
                    patch((d) => {
                      if (!id && !icon?.paths?.length) delete d.icons[name];
                      else d.icons[name] = { ...(id ? { assetId: id } : {}), ...(icon?.paths?.length ? { paths: icon.paths } : {}) };
                    })
                  }
                />
              </span>
              <Field
                label={t.themes.editor.iconPaths(name)}
                id={`field-icon-${name}`}
                rows={2}
                value={icon?.paths?.join("\n") ?? ""}
                placeholder="M2 8h12"
                onChange={(ev) =>
                  patch((d) => {
                    const paths = ev.target.value
                      .split("\n")
                      .map((line) => line.trim())
                      .filter(Boolean);
                    if (paths.length === 0 && !icon?.assetId) delete d.icons[name];
                    else d.icons[name] = { ...(icon?.assetId ? { assetId: icon.assetId } : {}), ...(paths.length ? { paths } : {}) };
                  })
                }
                className="w-72 font-mono"
              />
              {icon && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="mt-6"
                  onClick={() =>
                    patch((d) => {
                      delete d.icons[name];
                    })
                  }
                >
                  {t.themes.editor.reset}
                </Button>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}

export function BackdropTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  const backdrop = spec.backdrop ?? null;
  const effect: Effect = spec.effect === "constellation" || spec.effect === "confetti" ? spec.effect : "none";
  return (
    <Card className="p-4">
      <p className="mb-4 text-sm text-ink-muted">{t.themes.editor.backdropIntro}</p>
      <div className="flex flex-wrap items-end gap-4">
        <span className="space-y-1">
          <span className="block text-sm font-medium text-ink-muted">{t.themes.editor.picture}</span>
          <AssetSelect
            label={t.themes.editor.backdropPicture}
            assets={images}
            value={backdrop?.assetId}
            onChange={(id) =>
              patch((d) => {
                d.backdrop = id ? { assetId: id, fit: backdrop?.fit ?? "cover" } : null;
              })
            }
          />
        </span>
        {backdrop && (
          <span className="pb-0.5">
            <Segmented<"cover" | "tile">
              label={t.themes.editor.fit}
              size="sm"
              value={backdrop.fit}
              onChange={(fit) =>
                patch((d) => {
                  if (d.backdrop) d.backdrop.fit = fit;
                })
              }
              options={[
                { value: "cover", label: t.themes.editor.fitCover },
                { value: "tile", label: t.themes.editor.fitTile },
              ]}
            />
          </span>
        )}
      </div>
      <p className="mt-6 mb-2 text-sm text-ink-muted">{t.themes.editor.effectIntro}</p>
      <Segmented<Effect>
        label={t.themes.editor.effect}
        size="sm"
        value={effect}
        onChange={(next) =>
          patch((d) => {
            if (next === "none") delete d.effect;
            else d.effect = next;
          })
        }
        options={[
          { value: "none", label: t.themes.editor.effectNone, attrs: { "data-theme-effect": "none" } },
          { value: "constellation", label: t.themes.editor.effectConstellation, attrs: { "data-theme-effect": "constellation" } },
          { value: "confetti", label: t.themes.editor.effectConfetti, attrs: { "data-theme-effect": "confetti" } },
        ]}
      />
    </Card>
  );
}

export function FilesTab({ theme, spec, onNotice }: { theme?: Theme; spec: ThemeSpec; onNotice: (notice: string) => void }) {
  const uploadAsset = useUploadThemeAsset();
  const deleteAsset = useDeleteThemeAsset();
  const fileInput = useRef<HTMLInputElement>(null);
  const [tooBig, setTooBig] = useState(false);
  if (!theme) {
    return (
      <Card className="p-4">
        <p className="text-sm text-ink-muted">{t.themes.editor.saveFirst}</p>
      </Card>
    );
  }
  const used = new Set<string>([
    ...[spec.fonts.sans?.assetId, spec.fonts.mono?.assetId, spec.backdrop?.assetId].filter((id): id is string => Boolean(id)),
    ...Object.values(spec.cursors).map((c) => c.assetId),
    ...Object.values(spec.icons)
      .map((i) => i.assetId)
      .filter((id): id is string => Boolean(id)),
  ]);
  function onFile(file: File | undefined) {
    if (!file) return;
    if (file.size > THEME_ASSET_MAX_BYTES) {
      setTooBig(true);
      return;
    }
    setTooBig(false);
    uploadAsset.mutate({ id: theme!.id, file }, { onSuccess: (made) => onNotice(t.themes.editor.added(made.asset.name)) });
  }
  return (
    <Card className="space-y-3 p-4">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          variant="secondary"
          icon={<Icon.Upload />}
          loading={uploadAsset.isPending}
          onClick={() => fileInput.current?.click()}
          data-action="add-theme-file"
        >
          {t.themes.editor.addFile}
        </Button>
        <input
          ref={fileInput}
          type="file"
          accept={UPLOAD_ACCEPT}
          className="hidden"
          aria-label={t.themes.editor.chooseThemeFile}
          data-theme-file-input
          onChange={(ev) => onFile(ev.target.files?.[0])}
        />
        <p className="text-sm text-ink-subtle">{t.themes.editor.filesHint}</p>
      </div>
      {tooBig && <ErrorBanner>{t.themes.editor.tooBig}</ErrorBanner>}
      {uploadAsset.error && <ErrorBanner>{uploadAsset.error.message}</ErrorBanner>}
      {deleteAsset.error && <ErrorBanner>{deleteAsset.error.message}</ErrorBanner>}
      {theme.assets.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.themes.editor.noFilesYet}</p>
      ) : (
        <ul className="divide-y divide-border">
          {theme.assets.map((a) => (
            <li key={a.id} className="flex items-center gap-3 py-2 text-sm" data-theme-asset={a.name}>
              <span className="min-w-0 flex-1 truncate text-ink">{a.name}</span>
              <Tag>{a.contentType}</Tag>
              <span className="text-ink-subtle tabular-nums">{t.themes.editor.kilobytes(Math.max(1, Math.round(a.size / KILOBYTE)))}</span>
              {used.has(a.id) && <Tag>{t.themes.editor.inUse}</Tag>}
              <IconButton
                icon={<Icon.Trash />}
                label={t.themes.editor.removeFile(a.name)}
                size="sm"
                disabled={used.has(a.id) || deleteAsset.isPending}
                onClick={() => {
                  if (window.confirm(t.themes.editor.confirmRemoveFile(a.name))) {
                    deleteAsset.mutate({ id: theme.id, assetId: a.id }, { onSuccess: () => onNotice(t.themes.editor.removed(a.name)) });
                  }
                }}
                data-action="remove-theme-file"
              />
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

export function AdvancedTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  const size = new TextEncoder().encode(spec.css).length;
  return (
    <Card className="p-4">
      <Field
        label={t.themes.editor.extraCSS}
        rows={THEME_CSS_ROWS}
        value={spec.css}
        onChange={(ev) =>
          patch((d) => {
            d.css = ev.target.value;
          })
        }
        className="font-mono"
        hint={t.themes.editor.cssHint(Math.round(size / KILOBYTE), THEME_CSS_MAX_BYTES / KILOBYTE)}
        data-theme-css=""
      />
    </Card>
  );
}
