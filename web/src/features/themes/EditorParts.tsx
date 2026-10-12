import { useState } from "react";
import type { ThemeAsset, ThemeSpec } from "@/api/themes";
import { Button, Card, ColorField, Field, IconButton, SectionTitle, Segmented } from "@/components/ui";
import { Icon } from "@/components/icons";
import { THEME_COLOUR_PLACEHOLDER, THEME_CURSOR_PX, THEME_MAX_HOTSPOT, THEME_MAX_RADIUS, THEME_SHADOW_PLACEHOLDER } from "@/config";
import { t } from "@/i18n";
import { CURSOR_KINDS, SHADOW_KEYS, TOKEN_GROUPS, TOKEN_NAMES, tokenLabel } from "@/lib/theme-tokens";
import { AssetSelect, isFont, isImage, type Patch } from "./editorHelpers";

// The editor's tabs for the colours, the type, the shape and the cursors.

type Mode = "light" | "dark";

function stockValue(token: string): string {
  try {
    return getComputedStyle(document.documentElement).getPropertyValue(`--color-${token}`).trim();
  } catch {
    return "";
  }
}

export function ColoursTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  const [mode, setMode] = useState<Mode>("light");
  const set = spec.colors[mode];
  const overridden = Object.keys(set).length;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Segmented<Mode>
          label={t.themes.editor.palette}
          value={mode}
          onChange={setMode}
          options={[
            { value: "light", label: t.themes.editor.light, attrs: { "data-theme-mode": "light" } },
            { value: "dark", label: t.themes.editor.dark, attrs: { "data-theme-mode": "dark" } },
          ]}
        />
        <span className="text-sm text-ink-muted">
          {overridden === 0 ? t.themes.editor.nothingChanged : t.themes.editor.changed(overridden, TOKEN_NAMES.length)}
        </span>
      </div>
      {TOKEN_GROUPS.map((group) => (
        <Card key={group.id} className="p-4">
          <SectionTitle className="mb-3">{t.themes.groups[group.id]}</SectionTitle>
          <div className="grid gap-3 sm:grid-cols-2">
            {group.tokens.map((token) => {
              const value = set[token] ?? "";
              const label = tokenLabel(token);
              return (
                <ColorField
                  key={token}
                  id={`field-token-${token}`}
                  label={label}
                  value={value}
                  placeholder={stockValue(token) || THEME_COLOUR_PLACEHOLDER}
                  onChange={(next) =>
                    patch((draft) => {
                      if (next.trim() === "") delete draft.colors[mode][token];
                      else draft.colors[mode][token] = next.trim();
                    })
                  }
                  data-token={token}
                  data-token-mode={mode}
                >
                  {value && (
                    <IconButton
                      icon={<Icon.X />}
                      label={t.themes.editor.resetToken(label)}
                      size="sm"
                      onClick={() =>
                        patch((draft) => {
                          delete draft.colors[mode][token];
                        })
                      }
                      data-action="reset-token"
                    />
                  )}
                </ColorField>
              );
            })}
          </div>
        </Card>
      ))}
    </div>
  );
}

export function TypeTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const fonts = assets.filter(isFont);
  return (
    <Card className="space-y-5 p-4">
      {(["sans", "mono"] as const).map((role) => {
        const font = spec.fonts[role];
        const family = role === "sans" ? t.themes.editor.textFamily : t.themes.editor.codeFamily;
        return (
          <div key={role} className="space-y-2" data-theme-font={role}>
            <SectionTitle>{role === "sans" ? t.themes.editor.textFace : t.themes.editor.codeFace}</SectionTitle>
            <div className="flex flex-wrap items-end gap-3">
              <Field
                label={family}
                value={font?.family ?? ""}
                placeholder={role === "sans" ? "Inter" : "JetBrains Mono"}
                onChange={(ev) =>
                  patch((draft) => {
                    const next = ev.target.value;
                    if (next.trim() === "") delete draft.fonts[role];
                    else draft.fonts[role] = { ...(draft.fonts[role] ?? {}), family: next };
                  })
                }
                className="w-64"
                hint={t.themes.editor.familyHint}
              />
              <span className="space-y-1 pb-5">
                <span className="block text-sm font-medium text-ink-muted">{t.themes.editor.file}</span>
                <AssetSelect
                  label={t.themes.editor.fontFile(family)}
                  assets={fonts}
                  value={font?.assetId}
                  onChange={(id) =>
                    patch((draft) => {
                      const current = draft.fonts[role];
                      if (current) draft.fonts[role] = { family: current.family, ...(id ? { assetId: id } : {}) };
                    })
                  }
                />
              </span>
              {font && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="mb-5"
                  onClick={() =>
                    patch((draft) => {
                      delete draft.fonts[role];
                    })
                  }
                >
                  {t.themes.editor.reset}
                </Button>
              )}
            </div>
          </div>
        );
      })}
    </Card>
  );
}

function NumberField({
  label,
  value,
  onChange,
  max,
  placeholder,
}: {
  label: string;
  value: number | undefined;
  onChange: (value: number | undefined) => void;
  max: number;
  placeholder: string;
}) {
  return (
    <Field
      label={label}
      type="number"
      min={0}
      max={max}
      value={value ?? ""}
      placeholder={placeholder}
      onChange={(ev) => onChange(ev.target.value === "" ? undefined : Math.max(0, Math.min(max, Number(ev.target.value))))}
      className="w-28"
    />
  );
}

export function ShapeTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  return (
    <div className="space-y-4">
      <Card className="p-4">
        <SectionTitle className="mb-3">{t.themes.editor.corners}</SectionTitle>
        <div className="flex flex-wrap gap-4">
          <NumberField
            label={t.themes.editor.radiusControl}
            value={spec.shape.radiusControl}
            max={THEME_MAX_RADIUS}
            placeholder="6"
            onChange={(v) =>
              patch((d) => {
                if (v === undefined) delete d.shape.radiusControl;
                else d.shape.radiusControl = v;
              })
            }
          />
          <NumberField
            label={t.themes.editor.radiusOverlay}
            value={spec.shape.radiusOverlay}
            max={THEME_MAX_RADIUS}
            placeholder="8"
            onChange={(v) =>
              patch((d) => {
                if (v === undefined) delete d.shape.radiusOverlay;
                else d.shape.radiusOverlay = v;
              })
            }
          />
        </div>
      </Card>
      <Card className="p-4">
        <SectionTitle className="mb-3">{t.themes.editor.shadows}</SectionTitle>
        <div className="space-y-3">
          {SHADOW_KEYS.map((key) => (
            <Field
              key={key}
              label={t.themes.editor.shadowLabels[key] ?? key}
              value={spec.shadows[key] ?? ""}
              placeholder={THEME_SHADOW_PLACEHOLDER}
              onChange={(ev) =>
                patch((d) => {
                  if (ev.target.value.trim() === "") delete d.shadows[key];
                  else d.shadows[key] = ev.target.value;
                })
              }
              className="font-mono"
              data-shadow={key}
            />
          ))}
        </div>
      </Card>
    </div>
  );
}

export function CursorsTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  return (
    <Card className="p-4">
      <p className="mb-4 text-sm text-ink-muted">{t.themes.editor.cursorsIntro(THEME_CURSOR_PX)}</p>
      <div className="space-y-3">
        {CURSOR_KINDS.map((each) => {
          const cursor = spec.cursors[each.kind];
          const label = t.themes.cursors[each.kind] ?? each.kind;
          return (
            <div key={each.kind} className="flex flex-wrap items-end gap-3" data-theme-cursor={each.kind}>
              <span className="w-28 pb-2 text-sm text-ink">{label}</span>
              <span className="space-y-1">
                <span className="block text-sm font-medium text-ink-muted">{t.themes.editor.picture}</span>
                <AssetSelect
                  label={t.themes.editor.cursorPicture(label)}
                  assets={images}
                  value={cursor?.assetId}
                  onChange={(id) =>
                    patch((d) => {
                      if (!id) delete d.cursors[each.kind];
                      else d.cursors[each.kind] = { assetId: id, hotspotX: cursor?.hotspotX ?? 0, hotspotY: cursor?.hotspotY ?? 0 };
                    })
                  }
                />
              </span>
              {cursor && (
                <>
                  <NumberField
                    label={t.themes.editor.pointX}
                    value={cursor.hotspotX}
                    max={THEME_MAX_HOTSPOT}
                    placeholder="0"
                    onChange={(v) =>
                      patch((d) => {
                        d.cursors[each.kind]!.hotspotX = v ?? 0;
                      })
                    }
                  />
                  <NumberField
                    label={t.themes.editor.pointY}
                    value={cursor.hotspotY}
                    max={THEME_MAX_HOTSPOT}
                    placeholder="0"
                    onChange={(v) =>
                      patch((d) => {
                        d.cursors[each.kind]!.hotspotY = v ?? 0;
                      })
                    }
                  />
                </>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}
