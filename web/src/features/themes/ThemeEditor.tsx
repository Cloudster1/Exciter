import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useChooseTheme, useCreateTheme, useThemeExamples, useUpdateTheme, type Theme, type ThemeExample, type ThemeSpec } from "@/api/themes";
import { Button, Card, ErrorBanner, Field, OptionCard, SectionTitle, Switch, Tabs } from "@/components/ui";
import { THEME_PREVIEW_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { compileTheme, emptySpec } from "@/lib/theme-css";
import { AdvancedTab, BackdropTab, FilesTab, IconsTab } from "./EditorAssets";
import { ColoursTab, CursorsTab, ShapeTab, TypeTab } from "./EditorParts";
import { setThemePreview } from "./ThemeLoader";

type TabID = "colours" | "type" | "shape" | "cursors" | "icons" | "backdrop" | "files" | "advanced";

const tabs = (): Array<{ value: TabID; label: string }> => [
  { value: "colours", label: t.themes.editor.tabColours },
  { value: "type", label: t.themes.editor.tabType },
  { value: "shape", label: t.themes.editor.tabShape },
  { value: "cursors", label: t.themes.editor.tabCursors },
  { value: "icons", label: t.themes.editor.tabIcons },
  { value: "backdrop", label: t.themes.editor.tabBackdrop },
  { value: "files", label: t.themes.editor.tabFiles },
  { value: "advanced", label: t.themes.editor.tabAdvanced },
];

/** One theme, part by part. Nothing is written until Save; Preview shows the draft on this very page, and
 * leaving the page shows the chosen theme again. */
export function ThemeEditor({ theme }: { theme?: Theme }) {
  const navigate = useNavigate();
  const create = useCreateTheme();
  const update = useUpdateTheme();
  const choose = useChooseTheme();
  const [name, setName] = useState(theme?.name ?? "");
  const [shared, setShared] = useState(theme?.shared ?? false);
  const [spec, setSpec] = useState<ThemeSpec>(() => (theme ? structuredClone(theme.spec) : emptySpec()));
  const [tab, setTab] = useState<TabID>("colours");
  const [preview, setPreview] = useState(false);
  const [startedFrom, setStartedFrom] = useState("blank");
  const [notice, setNotice] = useState("");
  const assets = useMemo(() => theme?.assets ?? [], [theme?.assets]);
  const saving = create.isPending || update.isPending;
  const patch = (change: (draft: ThemeSpec) => void) =>
    setSpec((current) => {
      const draft = structuredClone(current);
      change(draft);
      return draft;
    });

  // The draft is compiled after it settles, so typing a colour does not
  // restyle the page on every keystroke.
  useEffect(() => {
    if (!preview) {
      setThemePreview(null);
      return;
    }
    const timer = window.setTimeout(
      () => setThemePreview(compileTheme({ id: theme?.id ?? "new", spec, assets }), spec.effect ?? null),
      THEME_PREVIEW_DEBOUNCE_MS,
    );
    return () => window.clearTimeout(timer);
  }, [preview, spec, theme?.id, assets]);
  useEffect(() => () => setThemePreview(null), []);

  function save(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    if (theme) {
      update.mutate({ id: theme.id, name: name.trim(), shared, spec }, { onSuccess: (saved) => setNotice(t.themes.saved(saved.theme.name)) });
    } else {
      create.mutate(
        { name: name.trim(), shared, spec },
        { onSuccess: (made) => navigate({ to: "/settings/themes/$themeId", params: { themeId: made.theme.id } }) },
      );
    }
  }

  const failure = create.error ?? update.error ?? choose.error;
  return (
    <form onSubmit={save} className="space-y-4" noValidate data-theme-editor={theme?.id ?? "new"}>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {notice && (
        <p role="status" className="text-sm text-ink-muted" data-theme-notice>
          {notice}
        </p>
      )}
      <Card className="p-4">
        <div className="flex flex-wrap items-end gap-4">
          <Field label={t.themes.editor.name} id="field-theme-name" value={name} onChange={(ev) => setName(ev.target.value)} required className="w-64" />
          <span className="flex items-center gap-2 pb-2 text-sm text-ink-muted">
            <Switch checked={shared} onChange={setShared} label={t.themes.editor.shared} data-theme-shared={shared ? "true" : "false"} />
            {t.themes.editor.shared}
          </span>
          <span className="flex items-center gap-2 pb-2 text-sm text-ink-muted">
            <Switch checked={preview} onChange={setPreview} label={t.themes.editor.preview} data-action="preview-theme" />
            {t.themes.editor.preview}
          </span>
          <span className="ml-auto flex gap-2 pb-1">
            {theme && !theme.active && (
              <Button
                type="button"
                variant="secondary"
                loading={choose.isPending}
                onClick={() => choose.mutate(theme.id, { onSuccess: () => setNotice(t.themes.nowUsing(theme.name)) })}
                data-action="use-theme"
              >
                {t.themes.use}
              </Button>
            )}
            <Button type="submit" loading={saving} disabled={!name.trim()} data-action="save-theme">
              {t.themes.editor.save}
            </Button>
          </span>
        </div>
      </Card>

      {!theme && (
        <StartFrom
          chosen={startedFrom}
          onChoose={(example) => {
            setStartedFrom(example?.key ?? "blank");
            setSpec(example ? structuredClone(example.spec) : emptySpec());
            if (example && !name.trim()) setName(example.name);
          }}
        />
      )}

      <Tabs<TabID>
        label={t.themes.editor.tabs}
        value={tab}
        onChange={setTab}
        tabs={tabs().map((each) => ({ ...each, attrs: { "data-theme-tab": each.value } }))}
      />

      {tab === "colours" && <ColoursTab spec={spec} patch={patch} />}
      {tab === "type" && <TypeTab spec={spec} patch={patch} assets={assets} />}
      {tab === "shape" && <ShapeTab spec={spec} patch={patch} />}
      {tab === "cursors" && <CursorsTab spec={spec} patch={patch} assets={assets} />}
      {tab === "icons" && <IconsTab spec={spec} patch={patch} assets={assets} />}
      {tab === "backdrop" && <BackdropTab spec={spec} patch={patch} assets={assets} />}
      {tab === "files" && <FilesTab theme={theme} spec={spec} onNotice={setNotice} />}
      {tab === "advanced" && <AdvancedTab spec={spec} patch={patch} />}
    </form>
  );
}

// A new theme starts blank or from one the product ships; picking one replaces the whole draft, so it comes
// before anything is typed.
function StartFrom({ chosen, onChoose }: { chosen: string; onChoose: (example: ThemeExample | null) => void }) {
  const { data } = useThemeExamples();
  const examples = data?.examples ?? [];
  if (examples.length === 0) return null;
  return (
    <Card className="p-4" data-theme-start>
      <SectionTitle className="mb-3">{t.themes.editor.startFrom}</SectionTitle>
      <div role="radiogroup" aria-label={t.themes.editor.startFrom} className="grid gap-3 sm:grid-cols-3">
        <OptionCard
          title={t.themes.editor.blank}
          description={t.themes.editor.blankBody}
          checked={chosen === "blank"}
          onSelect={() => onChoose(null)}
          data-theme-example="blank"
        />
        {examples.map((example) => (
          <OptionCard
            key={example.key}
            title={example.name}
            description={example.description}
            checked={chosen === example.key}
            onSelect={() => onChoose(example)}
            data-theme-example={example.key}
          />
        ))}
      </div>
    </Card>
  );
}
