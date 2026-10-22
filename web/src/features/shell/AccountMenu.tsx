import { useNavigate } from "@tanstack/react-router";
import { administers, useLogout, useMe } from "@/api/auth";
import { Avatar, IconButton, Menu, Tooltip, type MenuItem } from "@/components/ui";
import type { AnchoredSide } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { placesFor, SETTINGS_PATH } from "./places";
import { useConnectedTools } from "./tools";

/** The avatar's menu, "Your account": who is signed in, their own pages, the other tools, the settings for an
 *  administrator, and the way out. */
export function AccountMenu({ side = "right", align = "end" }: { side?: AnchoredSide; align?: "start" | "end" }) {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const logout = useLogout();
  const name = me?.user.name || me?.user.email || t.account.menu;
  const own: MenuItem[] = placesFor(me).flatMap((place) =>
    place.inAccountMenu
      ? [{ label: place.label, icon: <Glyph name={place.icon} />, onSelect: () => navigate({ to: place.path }), attrs: { "data-action": place.inAccountMenu } }]
      : [],
  );
  const tools: MenuItem[] = useConnectedTools().map((tool) => ({
    label: tool.name,
    icon: <img src={tool.icon} alt="" className="size-4 rounded-sm" />,
    href: tool.href,
    group: t.account.switchTo,
    attrs: { "data-tool": tool.id },
  }));
  const settings: MenuItem[] = administers(me?.role)
    ? [{ label: t.nav.settings, icon: <Icon.Settings />, onSelect: () => navigate({ to: SETTINGS_PATH }), attrs: { "data-action": "settings" } }]
    : [];
  const items: MenuItem[] = [
    ...own,
    ...tools,
    ...settings,
    {
      label: t.account.signOut,
      icon: <Icon.External />,
      // Wherever the request ends, the session is no longer this browser's to use.
      onSelect: () => logout.mutate(undefined, { onSettled: () => navigate({ to: "/login" }) }),
      disabled: !me,
      attrs: { "data-action": "sign-out" },
    },
  ];
  const header = me && (
    <div className="flex min-w-0 flex-col" data-account-header>
      <span className="truncate text-sm font-medium text-ink" data-account-name>
        {me.user.name || me.user.email}
      </span>
      {me.user.name && <span className="truncate text-xs text-ink-muted">{me.user.email}</span>}
      {me.role && <span className="text-xs text-ink-subtle">{t.members.roleNames[me.role]}</span>}
    </div>
  );
  return (
    <Menu
      label={t.account.menu}
      side={side}
      align={align}
      header={header}
      trigger={(props) => (
        <Tooltip text={t.account.menu} side={side === "top" ? "top" : "right"}>
          <IconButton
            icon={<Avatar name={name} src={me?.user.avatarUrl} size="sm" />}
            label={t.account.menu}
            size="sm"
            onClick={props.toggle}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            aria-controls={props["aria-controls"]}
            className="rounded-full"
            data-action="account"
          />
        </Tooltip>
      )}
      items={items}
    />
  );
}

function Glyph({ name }: { name: keyof typeof Icon }) {
  const Drawn = Icon[name];
  return <Drawn />;
}
