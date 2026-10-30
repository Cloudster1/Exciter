import { useRef, useState } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { themeExportHref, useActiveTheme, useChooseTheme, useDeleteTheme, useImportTheme, useThemes, useUpdateTheme, type Theme } from "@/api/themes";
import { useMe } from "@/api/auth";
import { FollowArmatureTheme } from "@/features/armature/FollowArmatureTheme";
import { Button, EmptyState, ErrorBanner, IconButton, Menu, PageHeader, Segmented, Table, Tag, Td, Th, useToast, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { THEMES_PATH } from "@/config";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { appRoute } from "./app";

type View = "mine" | "shared";

export const themesRoute = createRoute({ getParentRoute: () => appRoute, path: THEMES_PATH, component: ThemesPage });

/** Which themes a view shows. */
export function inThemeView(theme: Theme, view: View, me: string | undefined): boolean {
  if (view === "mine") return theme.ownerId === me;
  return theme.shared && theme.ownerId !== me;
}

/** What the page says about the theme the reader sees. */
export function activeThemeMeta(seen: Theme | null, source: string, orgDefault: Theme | undefined): string {
  if (seen && source === "everyone") return t.themes.usingDefault(seen.name);
  if (source === "armature") return seen ? t.themes.usingArmature(seen.name) : t.themes.usingArmatureBuiltIn;
  if (seen) return t.themes.using(seen.name);
  if (orgDefault) return t.themes.usingBuiltInOverDefault(orgDefault.name);
  return t.themes.usingBuiltIn;
}

/** Every theme the reader may use, and which one they do. */
function ThemesPage() {
  const { data, isLoading, error } = useThemes();
  const { data: viewer } = useMe();
  const me = viewer?.user.id;
  const administers = viewer?.administers ?? false;
  const { data: activeData } = useActiveTheme();
  const choose = useChooseTheme();
  const importTheme = useImportTheme();
  const update = useUpdateTheme();
  const remove = useDeleteTheme();
  const fileInput = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  const [view, setView] = useState<View>("mine");
  const toast = useToast();
  const notify = toast.success;
  const all = data?.themes ?? [];
  const themes = all.filter((each) => inThemeView(each, view, me));
  const seen = activeData?.theme ?? null;
  const source = activeData?.source ?? "";
  const orgDefault = all.find((each) => each.default);
  const failure = error ?? choose.error ?? update.error ?? importTheme.error ?? remove.error;

  return (
    <Page width="content">
      <PageHeader
        title={t.themes.title}
        meta={activeThemeMeta(seen, source, orgDefault)}
        actions={
          <>
            {seen && source === "everyone" && (
              <Button
                variant="secondary"
                onClick={() => choose.mutate({ builtIn: true }, { onSuccess: () => notify(t.themes.backToBuiltIn) })}
                data-action="built-in-theme"
              >
                {t.themes.useBuiltIn}
              </Button>
            )}
            {!seen && orgDefault && (
              <Button
                variant="secondary"
                onClick={() => choose.mutate(null, { onSuccess: () => notify(t.themes.nowUsing(orgDefault.name)) })}
                data-action="org-default-theme"
              >
                {t.themes.useOrgDefault}
              </Button>
            )}
            <Button
              variant="secondary"
              icon={<Icon.Upload />}
              loading={importTheme.isPending}
              onClick={() => fileInput.current?.click()}
              data-action="import-theme"
            >
              {t.themes.importTheme}
            </Button>
            <input
              ref={fileInput}
              type="file"
              accept="application/json,.json"
              className="sr-only"
              aria-label={t.themes.importFile}
              data-theme-file
              onChange={(event) => {
                const file = event.target.files?.[0];
                event.target.value = "";
                if (file) importTheme.mutate(file, { onSuccess: (made) => notify(t.themes.imported(made.theme.name)) });
              }}
            />
            <Button icon={<Icon.Plus />} onClick={() => navigate({ to: "/settings/themes/new" })} data-action="new-theme">
              {t.themes.newTheme}
            </Button>
          </>
        }
      />
      <FollowArmatureTheme />
      <div className="mb-4">
        <Segmented<View>
          label={t.themes.show}
          value={view}
          onChange={setView}
          options={[
            { value: "mine", label: t.themes.viewMine, attrs: { "data-themes-view": "mine" } },
            { value: "shared", label: t.themes.viewShared, attrs: { "data-themes-view": "shared" } },
          ]}
        />
      </div>
      <div className="space-y-3">
        {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
        {isLoading ? null : themes.length === 0 ? (
          <EmptyState
            icon={<Icon.Palette />}
            title={view === "mine" ? t.themes.emptyMine : t.themes.emptyShared}
            description={t.themes.emptyBody}
            action={
              view === "mine" ? (
                <Button variant="secondary" onClick={() => navigate({ to: "/settings/themes/new" })}>
                  {t.themes.makeOne}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th>{t.themes.columnTheme}</Th>
                <Th>{t.themes.columnOwner}</Th>
                <Th>{t.themes.columnInUse}</Th>
                <Th className="w-10">
                  <span className="sr-only">{t.themes.columnActions}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {themes.map((theme) => (
                <ThemeRow
                  key={theme.id}
                  theme={theme}
                  me={me}
                  administers={administers}
                  inUse={seen?.id === theme.id}
                  orgDefault={orgDefault}
                  actions={{ choose, update, remove }}
                  onNotice={notify}
                />
              ))}
            </tbody>
          </Table>
        )}
      </div>
    </Page>
  );
}

interface RowActions {
  choose: ReturnType<typeof useChooseTheme>;
  update: ReturnType<typeof useUpdateTheme>;
  remove: ReturnType<typeof useDeleteTheme>;
}

/** One theme in the list, with what the reader may do to it. */
function ThemeRow({
  theme,
  me,
  administers,
  inUse,
  orgDefault,
  actions: { choose, update, remove },
  onNotice: notify,
}: {
  theme: Theme;
  me: string | undefined;
  administers: boolean;
  inUse: boolean;
  orgDefault: Theme | undefined;
  actions: RowActions;
  onNotice: (message: string) => void;
}) {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const editable = theme.ownerId === me || (administers && theme.shared);
  return (
    <tr key={theme.id} data-theme-row={theme.name} data-theme-active={theme.active ? "true" : "false"} data-theme-default={theme.default ? "true" : "false"}>
      <Td>
        {editable ? (
          <Link to="/settings/themes/$themeId" params={{ themeId: theme.id }} className="font-medium text-ink hover:text-accent">
            {theme.name}
          </Link>
        ) : (
          <span className="font-medium text-ink">{theme.name}</span>
        )}
        {theme.shared && <Tag className="ml-2">{t.themes.tagShared}</Tag>}
        {theme.default && <Tag className="ml-2">{t.themes.tagDefault}</Tag>}
        {inUse && <Tag className="ml-2 text-accent">{t.themes.tagInUse}</Tag>}
      </Td>
      <Td className="text-ink-muted">{theme.ownerId === me ? t.themes.you : theme.ownerName}</Td>
      <Td className="text-ink-muted tabular-nums">{t.themes.people(theme.inUse)}</Td>
      <Td className="text-right">
        <Menu
          label={t.themes.actionsFor(theme.name)}
          align="end"
          trigger={(props) => (
            <IconButton
              icon={<Icon.More />}
              label={t.themes.actionsFor(theme.name)}
              size="sm"
              onClick={props.toggle}
              aria-haspopup={props["aria-haspopup"]}
              aria-expanded={props["aria-expanded"]}
              data-action="theme-menu"
            />
          )}
          items={[
            theme.active
              ? {
                  label: t.themes.stopUsing,
                  icon: <Icon.X />,
                  onSelect: () => choose.mutate(null, { onSuccess: () => notify(orgDefault ? t.themes.backToDefault : t.themes.backToBuiltIn) }),
                  attrs: { "data-action": "stop-theme" },
                }
              : {
                  label: t.themes.use,
                  icon: <Icon.Check />,
                  onSelect: () => choose.mutate(theme.id, { onSuccess: () => notify(t.themes.nowUsing(theme.name)) }),
                  attrs: { "data-action": "use-theme" },
                },
            {
              label: t.themes.edit,
              icon: <Icon.Edit />,
              disabled: !editable,
              onSelect: () => navigate({ to: "/settings/themes/$themeId", params: { themeId: theme.id } }),
              attrs: { "data-action": "edit-theme" },
            },
            {
              label: t.themes.exportFile,
              icon: <Icon.Download />,
              onSelect: () => {
                window.location.href = themeExportHref(theme.id);
              },
              attrs: { "data-action": "export-theme" },
            },
            {
              label: theme.shared ? t.themes.stopSharing : t.themes.share,
              icon: <Icon.Share />,
              disabled: !editable,
              onSelect: () =>
                update.mutate(
                  { id: theme.id, shared: !theme.shared },
                  { onSuccess: () => notify(theme.shared ? t.themes.privateNow(theme.name) : t.themes.sharedNow(theme.name)) },
                ),
              attrs: { "data-action": "share-theme" },
            },
            {
              label: t.themes.delete,
              icon: <Icon.Trash />,
              danger: true,
              disabled: !editable,
              onSelect: async () => {
                const noun = t.kit.confirm.things.named(t.kit.confirm.things.theme, theme.name);
                if (await confirm({ noun, body: t.themes.confirmDelete(theme.inUse) })) {
                  remove.mutate(theme.id, { onSuccess: () => notify(t.themes.deleted(theme.name)) });
                }
              },
              attrs: { "data-action": "delete-theme" },
            },
          ]}
        />
      </Td>
    </tr>
  );
}
