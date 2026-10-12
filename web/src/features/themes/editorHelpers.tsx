import type { ThemeAsset, ThemeSpec } from "@/api/themes";
import { SelectInput } from "@/components/ui";
import { t } from "@/i18n";

/** A change to the draft, made on a copy of it. */
export type Patch = (change: (draft: ThemeSpec) => void) => void;

export function isFont(a: ThemeAsset): boolean {
  return a.contentType.startsWith("font/");
}
export function isImage(a: ThemeAsset): boolean {
  return a.contentType.startsWith("image/");
}

export function AssetSelect({
  label,
  assets,
  value,
  onChange,
}: {
  label: string;
  assets: ThemeAsset[];
  value: string | undefined;
  onChange: (id: string | undefined) => void;
}) {
  return (
    <SelectInput aria-label={label} controlSize="sm" value={value ?? ""} onChange={(ev) => onChange(ev.target.value || undefined)} className="w-56">
      <option value="">{assets.length === 0 ? t.themes.editor.noFiles : t.themes.editor.chooseFile}</option>
      {assets.map((a) => (
        <option key={a.id} value={a.id}>
          {a.name}
        </option>
      ))}
    </SelectInput>
  );
}
