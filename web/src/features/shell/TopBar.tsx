import { useEffect, type ReactNode } from "react";
import { Link, useNavigate, useRouterState } from "@tanstack/react-router";
import { useLogout, useMe, useSetLanguage, useSwitchOrg } from "@/api/auth";
import { Avatar, Breadcrumbs, Button, IconButton, Menu, type Crumb, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { THEMES_PATH, TOKENS_PATH } from "@/config";
import { LANGUAGES, t, type LanguageChoice } from "@/i18n";
import { crumbsFor, placeAbove } from "./places";
import { DRAWER_ID } from "./state";

// Ctrl or Cmd+K anywhere in the shell, fields included, as in Armature and
// Stator: it is the product's one shortcut and a field has no other use for it.
export function useSwitcherShortcut(onOpen: () => void) {
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && !event.repeat && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onOpen();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onOpen]);
}

/** Above the content: the drawer's button on a narrow screen, where the reader is, the quick switcher, and the person signed in. */
export function TopBar({
  narrow,
  drawerOpen,
  onOpenDrawer,
  switching,
  onSwitch,
}: {
  narrow: boolean;
  drawerOpen: boolean;
  onOpenDrawer: () => void;
  switching: boolean;
  onSwitch: () => void;
}) {
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-surface px-2 shell:gap-3 shell:px-4" data-top-bar data-print-hide>
      {narrow && (
        <IconButton
          icon={<Icon.Menu />}
          label={t.nav.openDrawer}
          size="md"
          onClick={onOpenDrawer}
          aria-haspopup="dialog"
          aria-expanded={drawerOpen}
          aria-controls={drawerOpen ? DRAWER_ID : undefined}
          data-action="drawer"
        />
      )}
      <PlaceCrumbs />
      <div className="ml-auto flex min-w-0 items-center gap-1 shell:gap-2">
        <button
          type="button"
          onClick={onSwitch}
          aria-haspopup="dialog"
          aria-expanded={switching}
          aria-label={t.switcher.open}
          className="flex h-8 items-center gap-2 rounded-control border border-border-strong/70 bg-canvas px-2.5 text-sm text-ink-subtle hover:border-border-strong shell:w-56"
          data-action="switcher"
        >
          <Icon.Search className="shrink-0" />
          <span className="hidden min-w-0 flex-1 truncate text-left shell:inline">{t.switcher.placeholder}</span>
          <kbd className="hidden font-mono text-2xs shell:inline">{t.switcher.shortcut}</kbd>
        </button>
        <OrgSwitcher />
        <AccountMenu />
      </div>
    </header>
  );
}

/** Where the reader is: the organization, then the page's section and the page itself, or the place it lies under. */
function PlaceCrumbs() {
  const { data: me } = useMe();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const trail = crumbsFor(pathname);
  const above = trail.length === 0 ? placeAbove(pathname) : undefined;
  const crumbs: Crumb[] = [
    { label: me?.organization?.name ?? "", render: (label) => <Link to="/">{label}</Link> },
    ...trail.map((label, index) => ({
      label,
      render: index === trail.length - 1 ? (shown: ReactNode) => <span aria-current="page">{shown}</span> : undefined,
    })),
    ...(above
      ? [...(above.section ? [{ label: above.section }] : []), { label: above.label, render: (shown: ReactNode) => <Link to={above.path}>{shown}</Link> }]
      : []),
  ];
  if (!me?.organization) return null;
  return <Breadcrumbs crumbs={crumbs} className="hidden min-w-0 truncate sm:block [&_a]:hover:text-ink" />;
}

/** The person's other organizations, for one who belongs to more than one. */
function OrgSwitcher() {
  const { data: me } = useMe();
  const switchOrg = useSwitchOrg();
  const organizations = me?.organizations ?? [];
  if (organizations.length < 2) return null;
  const current = me?.organization?.slug;
  return (
    <Menu
      label={t.account.switchOrg}
      align="end"
      trigger={(props) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={props.toggle}
          aria-haspopup={props["aria-haspopup"]}
          aria-expanded={props["aria-expanded"]}
          aria-controls={props["aria-controls"]}
          aria-label={t.account.switchOrgFrom(me?.organization?.name ?? "")}
          iconRight={<Icon.ChevronDown className="shrink-0 text-ink-subtle" />}
          data-action="switch-org"
        >
          <span className="hidden max-w-40 truncate shell:inline">{me?.organization?.name}</span>
        </Button>
      )}
      items={organizations.map((org) => ({
        label: org.orgName,
        checked: org.orgSlug === current,
        onSelect: () => org.orgSlug !== current && switchOrg.mutate(org.orgSlug),
        attrs: { "data-org": org.orgSlug },
      }))}
    />
  );
}

/** Who is signed in, their themes, the language they read in, and the way out. */
function AccountMenu() {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const logout = useLogout();
  const setLanguage = useSetLanguage();
  const name = me?.user.name || me?.user.email || t.account.menu;
  const chosen: LanguageChoice = me?.user.locale ?? "";
  const languages: MenuItem[] = (["", ...LANGUAGES] as const).map((choice) => ({
    label: choice ? t.account.languages[choice] : t.account.browserLanguage,
    checked: chosen === choice,
    onSelect: () => choice !== chosen && setLanguage.mutate(choice),
    disabled: !me,
    attrs: { "data-language": choice || "browser" },
  }));
  const items: MenuItem[] = [
    { label: t.account.themes, icon: <Icon.Palette />, onSelect: () => navigate({ to: THEMES_PATH }), attrs: { "data-action": "themes" } },
    { label: t.account.tokens, icon: <Icon.Key />, onSelect: () => navigate({ to: TOKENS_PATH }), attrs: { "data-action": "tokens" } },
    ...languages,
    {
      label: t.account.signOut,
      icon: <Icon.External />,
      // Wherever the request ends, the session is no longer this browser's to use.
      onSelect: () => logout.mutate(undefined, { onSettled: () => navigate({ to: "/login" }) }),
      disabled: !me,
      attrs: { "data-action": "sign-out" },
    },
  ];
  return (
    <Menu
      label={t.account.menu}
      align="end"
      trigger={(props) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={props.toggle}
          aria-haspopup={props["aria-haspopup"]}
          aria-expanded={props["aria-expanded"]}
          aria-controls={props["aria-controls"]}
          aria-label={t.account.menu}
          className="px-1!"
          data-action="account"
        >
          <Avatar name={name} src={me?.user.avatarUrl} size="sm" />
          <span className="hidden text-sm text-ink shell:inline" data-account-name>
            {name}
          </span>
        </Button>
      )}
      items={items}
    />
  );
}
