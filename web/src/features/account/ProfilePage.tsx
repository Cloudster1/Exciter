import { useEffect, useState, type FormEvent } from "react";
import { useMe, useSaveReading } from "@/api/auth";
import { Button, Card, ErrorBanner, Labelled, PageHeader, SearchPicker, SectionTitle, Select, Skeleton, Page } from "@/components/ui";
import { TIME_ZONE_MATCHES_MAX } from "@/config";
import { browserLanguage, isLanguage, LANGUAGES, resolveLanguage, t, useLanguage, type LanguageChoice } from "@/i18n";
import { browserTimeZone, isTimeZone, timeZone, timeZones } from "@/lib/format";

/** Your profile (#280): who the person is here, and the language and time zone Exciter writes in for them. */
export function ProfilePage() {
  const { data: me } = useMe();
  if (!me) return <Skeleton />;
  return (
    <Page width="narrow" className="space-y-6" data-profile>
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
      <ReadingSection language={me.user.locale ?? ""} zone={me.user.timezone ?? ""} />
    </Page>
  );
}

/** A zone as a person reads it: America/New York rather than America/New_York. */
function zoneName(zone: string): string {
  return zone.replace(/_/g, " ");
}

// The zones whose name holds what was typed, a few at most, with the chosen one
// among them so the select can show it; nothing typed lists only the choice.
function zoneMatches(words: string, chosen: string): string[] {
  const wanted = words.trim().toLowerCase().replace(/_/g, " ");
  const found = wanted
    ? timeZones()
        .filter((zone) => zoneName(zone).toLowerCase().includes(wanted))
        .slice(0, TIME_ZONE_MATCHES_MAX)
    : [];
  return chosen && !found.includes(chosen) ? [chosen, ...found] : found;
}

// A new language or zone draws the page again from scratch, so the notice that
// the choice was saved has to outlive the component that made it.
let savedNotice = false;

/** The language and the time zone, chosen together and kept by one Save. */
function ReadingSection({ language, zone }: { language: LanguageChoice; zone: string }) {
  const active = useLanguage();
  const save = useSaveReading();
  const [choice, setChoice] = useState<LanguageChoice>(language);
  const [chosenZone, setChosenZone] = useState(zone);
  const [zoneWords, setZoneWords] = useState("");
  const [saved, setSaved] = useState(savedNotice);
  useEffect(() => {
    savedNotice = false;
  }, []);
  const zones = zoneMatches(zoneWords, chosenZone);
  const previewZone = isTimeZone(chosenZone) ? chosenZone : browserTimeZone();
  const preview = new Intl.DateTimeFormat(resolveLanguage(choice), { dateStyle: "medium", timeStyle: "short", timeZone: previewZone }).format(new Date());

  function submit(event: FormEvent) {
    event.preventDefault();
    setSaved(false);
    savedNotice = resolveLanguage(choice) !== active || previewZone !== timeZone();
    save.mutate(
      { locale: choice, timezone: chosenZone },
      {
        onSuccess: () => setSaved(true),
        onError: () => {
          savedNotice = false;
        },
      },
    );
  }

  return (
    <section aria-labelledby="profile-language" data-profile-language="">
      <Card className="p-4">
        <form onSubmit={submit} className="space-y-3" noValidate>
          <SectionTitle id="profile-language">{t.profile.language}</SectionTitle>
          <p className="text-sm text-ink-muted">{t.profile.languageHint}</p>
          {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
          <Select
            id="field-language"
            label={t.profile.languageField}
            value={choice}
            disabled={save.isPending}
            onChange={(event) => {
              setSaved(false);
              setChoice(isLanguage(event.target.value) ? event.target.value : "");
            }}
            data-field="language"
          >
            <option value="">{t.profile.languageBrowser(t.account.languages[browserLanguage()])}</option>
            {LANGUAGES.map((each) => (
              <option key={each} value={each} lang={each}>
                {t.account.languages[each]}
              </option>
            ))}
          </Select>
          <Labelled id="field-timezone" label={t.profile.timezoneField}>
            <SearchPicker
              id="field-timezone"
              aria-label={t.profile.timezoneField}
              value={chosenZone}
              onChange={(next) => {
                setSaved(false);
                setChosenZone(next);
              }}
              words={zoneWords}
              onWords={setZoneWords}
              options={zones.map((each) => ({ value: each, label: zoneName(each) }))}
              leading={<option value="">{t.profile.timezoneBrowser(zoneName(browserTimeZone()))}</option>}
              placeholder={t.profile.timezoneSearch}
              nothing={zoneWords.trim() && zones.every((each) => each === chosenZone) ? t.profile.timezoneNone : undefined}
              disabled={save.isPending}
              controlSize="md"
            />
          </Labelled>
          <p className="text-sm text-ink-muted" data-format-preview="">
            {t.profile.preview(preview)}
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" loading={save.isPending} data-action="save-reading">
              {t.profile.save}
            </Button>
            {saved && (
              <p role="status" className="text-sm text-ink-muted" data-language-saved="">
                {t.profile.saved}
              </p>
            )}
          </div>
        </form>
      </Card>
    </section>
  );
}
