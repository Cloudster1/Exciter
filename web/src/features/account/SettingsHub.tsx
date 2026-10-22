import { Link } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { Card, PageHeader, SectionTitle } from "@/components/ui";
import { Icon } from "@/components/icons";
import { placesFor, type Place } from "@/features/shell/places";
import { t } from "@/i18n";

/** The settings area's hub (#280): the person's own pages, and the installation's for its administrators. */
export function SettingsHub() {
  const { data: me } = useMe();
  const places = placesFor(me);
  const settings = places.filter((place) => place.area === "settings");
  return (
    <div className="mx-auto max-w-3xl space-y-6" data-settings-hub>
      <PageHeader title={t.settingsHub.title} />
      <HubCard id="you" title={t.settingsHub.you} intro={t.settingsHub.youIntro} places={places.filter((place) => place.area === "you")} />
      {settings.length > 0 && <HubCard id="settings" title={t.settingsHub.settings} intro={t.settingsHub.settingsIntro} places={settings} />}
    </div>
  );
}

function HubCard({ id, title, intro, places }: { id: string; title: string; intro: string; places: Place[] }) {
  return (
    <Card className="space-y-3 p-4" data-hub-card={id}>
      <SectionTitle id={`hub-${id}`}>{title}</SectionTitle>
      <p className="text-sm text-ink-muted">{intro}</p>
      <ul aria-labelledby={`hub-${id}`} className="grid gap-1 sm:grid-cols-2">
        {places.map((place) => {
          const Glyph = Icon[place.icon];
          return (
            <li key={place.path}>
              <Link to={place.path} className="flex h-8 items-center gap-2 rounded-control px-2 text-sm text-ink hover:bg-surface-raised hover:text-accent">
                <Glyph className="shrink-0 text-ink-muted" />
                <span className="min-w-0 truncate">{place.label}</span>
              </Link>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}
