import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useSetDefaultTheme, useThemes } from "@/api/themes";
import { Card, ErrorBanner, OptionCard, PageHeader, Skeleton } from "@/components/ui";
import { THEMES_PATH } from "@/config";
import { t } from "@/i18n";

/** The theme the installation shows to whoever has not chosen one: the built-in one or a shared theme. */
export function OrgDefaultTheme() {
  const { data, isLoading, error } = useThemes();
  const setDefault = useSetDefaultTheme();
  const [notice, setNotice] = useState("");
  const shared = (data?.themes ?? []).filter((each) => each.shared);
  const current = shared.find((each) => each.default)?.id ?? null;
  const failure = error ?? setDefault.error;

  function choose(id: string | null, done: string) {
    if (id === current || setDefault.isPending) return;
    setDefault.mutate(id, { onSuccess: () => setNotice(done) });
  }

  return (
    <div className="mx-auto max-w-3xl" data-default-theme>
      <PageHeader title={t.themes.orgDefault.title} />
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{t.themes.orgDefault.intro}</p>
        {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
        {notice && (
          <p role="status" className="text-sm text-ink-muted" data-default-theme-notice>
            {notice}
          </p>
        )}
        {isLoading ? (
          <Skeleton />
        ) : (
          <div role="radiogroup" aria-label={t.themes.orgDefault.choices} className="grid gap-3 sm:grid-cols-2">
            <OptionCard
              title={t.themes.orgDefault.builtIn}
              description={t.themes.orgDefault.builtInBody}
              checked={current === null}
              onSelect={() => choose(null, t.themes.defaultCleared)}
              data-default-theme-choice="built-in"
            />
            {shared.map((theme) => (
              <OptionCard
                key={theme.id}
                title={theme.name}
                description={t.themes.orgDefault.by(theme.ownerName)}
                checked={current === theme.id}
                onSelect={() => choose(theme.id, t.themes.defaultSet(theme.name))}
                data-default-theme-choice={theme.name}
              />
            ))}
          </div>
        )}
        {!isLoading && shared.length === 0 && (
          <p className="text-sm text-ink-muted">
            {t.themes.orgDefault.noneShared}{" "}
            <Link to={THEMES_PATH} className="text-accent underline underline-offset-2">
              {t.themes.orgDefault.openThemes}
            </Link>
          </p>
        )}
      </Card>
    </div>
  );
}
