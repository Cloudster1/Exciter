import { useMe, useSetLanguage } from "@/api/auth";
import { Card, ErrorBanner, PageHeader, Radio, SectionTitle, Skeleton } from "@/components/ui";
import { LANGUAGES, t, type LanguageChoice } from "@/i18n";

/** Your profile (#280): who the person is here, and the language Exciter speaks to them in, saved to the account. */
export function ProfilePage() {
  const { data: me } = useMe();
  const setLanguage = useSetLanguage();
  if (!me) return <Skeleton />;
  const chosen: LanguageChoice = me.user.locale ?? "";
  return (
    <div className="mx-auto max-w-2xl space-y-6" data-profile>
      <PageHeader title={t.profile.title} meta={t.profile.intro} />
      <Card className="space-y-3 p-4">
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-ink-muted">{t.profile.name}</dt>
          <dd className="min-w-0 truncate text-ink" data-profile-name>
            {me.user.name}
          </dd>
          <dt className="text-ink-muted">{t.profile.email}</dt>
          <dd className="min-w-0 truncate text-ink">{me.user.email}</dd>
          {me.role && (
            <>
              <dt className="text-ink-muted">{t.profile.role}</dt>
              <dd className="text-ink">{t.members.roleNames[me.role]}</dd>
            </>
          )}
        </dl>
        <p className="text-xs text-ink-subtle">{t.profile.identity}</p>
      </Card>
      <Card className="space-y-3 p-4">
        <fieldset className="space-y-2" data-language-choice>
          <legend>
            <SectionTitle>{t.profile.language}</SectionTitle>
          </legend>
          <p className="text-sm text-ink-muted">{t.profile.languageHint}</p>
          {(["", ...LANGUAGES] as const).map((choice) => (
            <Radio
              key={choice || "browser"}
              name="language"
              value={choice}
              label={choice ? t.account.languages[choice] : t.account.browserLanguage}
              checked={chosen === choice}
              disabled={setLanguage.isPending}
              onChange={() => choice !== chosen && setLanguage.mutate(choice)}
              data-language={choice || "browser"}
            />
          ))}
        </fieldset>
        {setLanguage.error && <ErrorBanner>{setLanguage.error.message}</ErrorBanner>}
        <p role="status" className="text-sm text-ink-muted empty:hidden">
          {setLanguage.isSuccess ? t.profile.saved : ""}
        </p>
      </Card>
    </div>
  );
}
