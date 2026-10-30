import { Link } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { Card, PageHeader, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { placesFor } from "@/features/shell/places";
import { t } from "@/i18n";

/** The settings area's hub (#280): the person's own pages, each with what it is for. The installation's are under Admin. */
export function SettingsHub() {
  const { data: me } = useMe();
  const you = placesFor(me).filter((place) => place.area === "you");
  return (
    <Page width="narrow" data-settings-hub>
      <PageHeader title={t.settingsHub.title} />
      <p className="mb-3 text-sm text-ink-muted">{t.settingsHub.youIntro}</p>
      <Card className="divide-y divide-border" data-hub-card="you" aria-label={t.settingsHub.you} role="region">
        {you.map((place) => {
          const Glyph = Icon[place.icon];
          return (
            <Link
              key={place.id}
              to={place.path}
              className="flex items-center gap-3 px-4 py-3 text-ink no-underline hover:bg-surface-raised"
              data-settings-link={place.id}
            >
              <Glyph className="shrink-0 text-ink-muted" />
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium text-ink">{place.label}</span>
                <span className="block text-sm text-ink-muted">{place.detail}</span>
              </span>
              <Icon.ChevronDown className="shrink-0 -rotate-90 text-ink-subtle" />
            </Link>
          );
        })}
      </Card>
    </Page>
  );
}
