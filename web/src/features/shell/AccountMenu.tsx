import { useNavigate } from "@tanstack/react-router";
import { useLogout, useMe } from "@/api/auth";
import { Avatar, IconButton, Menu, type MenuEntry, type MenuItem } from "@/components/ui";
import type { AnchoredSide } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { adminEntry, placesFor } from "./places";
import { useConnectedTools } from "./tools";

/** The avatar's menu, "Your account": who is signed in, their own pages, the other tools, Admin for an
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
    icon: <img src={tool.icon} alt="" width={16} height={16} className="size-4 rounded-sm" />,
    href: tool.href,
    attrs: { "data-tool": tool.id },
  }));
  const admin = adminEntry();
  const settings: MenuItem[] = me?.administers
    ? [
        {
          label: admin.label,
          icon: <Icon.Settings />,
          onSelect: () => navigate({ to: admin.path }),
          separated: true,
          attrs: { "data-action": "admin" },
        },
      ]
    : [];
  const items: MenuEntry[] = [
    ...own,
    ...(tools.length > 0 ? [{ group: t.account.switchTo, items: tools, attrs: { "data-switch-to": "" } }] : []),
    ...settings,
    {
      label: t.account.signOut,
      icon: <Icon.External />,
      // Wherever the request ends, the session is no longer this browser's to use.
      onSelect: () => logout.mutate(undefined, { onSettled: () => navigate({ to: "/login" }) }),
      disabled: !me,
      separated: true,
      attrs: { "data-action": "sign-out" },
    },
  ];
  const header = me && (
    <div className="mb-1 flex items-center gap-2 border-b border-border px-2 pt-1 pb-2" data-account-header>
      <Avatar name={name} src={me.user.avatarUrl} size="md" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium text-ink" data-account-name>
          {me.user.name || me.user.email}
        </span>
        {me.user.name && <span className="block truncate text-xs text-ink-muted">{me.user.email}</span>}
        {me.role && (
          <span className="block text-2xs text-ink-subtle" data-account-role={me.role}>
            {me.administers ? t.members.roleNames.admin : t.members.roleNames[me.role]}
          </span>
        )}
      </span>
    </div>
  );
  return (
    <Menu
      label={t.account.menu}
      side={side}
      align={align}
      // As wide as the rail, so the list opens past the rail's edge rather than over it.
      className={side === "right" ? "w-full justify-center" : undefined}
      header={header}
      // No tooltip: focus comes back to the avatar when the menu closes, and a tooltip shown then sits outside
      // every landmark. The button's title names it to the pointer.
      trigger={(props) => (
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
      )}
      items={items}
    />
  );
}

function Glyph({ name }: { name: keyof typeof Icon }) {
  const Drawn = Icon[name];
  return <Drawn />;
}
