import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useInstallation, useRenameInstallation, type Installation } from "@/api/installation";
import { useSetDefaultTheme, useThemes } from "@/api/themes";
import { Button, Card, ErrorBanner, Field, SelectInput, Skeleton, useToast } from "@/components/ui";
import { t } from "@/i18n";
import { SuiteDemo } from "./SuiteDemo";

/** The installation's name, its default theme and the suite demo, for administrators to change. */
export function InstallationSettings() {
  const { data, isLoading, error } = useInstallation();
  // Held here rather than in the form: a rename remounts the form, and what it
  // says about that rename has to outlive the remount.
  const rename = useRenameInstallation();
  const [notice, setNotice] = useState("");
  if (isLoading) return <Skeleton />;
  if (error || !data) return <ErrorBanner>{error?.message}</ErrorBanner>;
  return (
    <div className="space-y-6">
      <NameForm key={data.name} installation={data} rename={rename} notice={notice} setNotice={setNotice} />
      <DefaultTheme />
      <SuiteDemo />
    </div>
  );
}

/** The theme everybody sees until they choose one of their own; any theme shared with everyone can be it. */
function DefaultTheme() {
  const { data } = useThemes();
  const setDefault = useSetDefaultTheme();
  const toast = useToast();
  const shared = (data?.themes ?? []).filter((theme) => theme.shared);
  const current = shared.find((theme) => theme.default);

  function choose(id: string) {
    const theme = shared.find((each) => each.id === id);
    setDefault.mutate(theme ? theme.id : null, {
      onSuccess: () => toast.success(theme ? t.themes.defaultSet(theme.name) : t.themes.defaultCleared),
    });
  }

  return (
    <Card className="space-y-3 p-4" data-default-theme={current?.name ?? ""}>
      <h2 className="text-sm font-semibold text-ink">{t.admin.installation.defaultTheme}</h2>
      <p className="text-sm text-ink-muted">{t.admin.installation.defaultThemeIntro}</p>
      {data && shared.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.admin.installation.noShared}</p>
      ) : (
        <SelectInput
          aria-label={t.admin.installation.defaultTheme}
          value={current?.id ?? ""}
          disabled={!data || setDefault.isPending}
          onChange={(event) => choose(event.target.value)}
          data-default-theme-select
        >
          <option value="">{t.admin.installation.builtIn}</option>
          {shared.map((theme) => (
            <option key={theme.id} value={theme.id}>
              {theme.name}
            </option>
          ))}
        </SelectInput>
      )}
      {setDefault.error && <ErrorBanner>{setDefault.error.message}</ErrorBanner>}
    </Card>
  );
}

function NameForm({
  installation,
  rename,
  notice,
  setNotice,
}: {
  installation: Installation;
  rename: ReturnType<typeof useRenameInstallation>;
  notice: string;
  setNotice: (notice: string) => void;
}) {
  const [name, setName] = useState(installation.name);
  const fields = rename.error instanceof ApiError ? rename.error.fields : {};
  const formError = rename.error && Object.keys(fields).length === 0 ? rename.error.message : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    rename.mutate(name, { onSuccess: () => setNotice(t.installation.saved) });
  }

  return (
    <form onSubmit={submit} noValidate aria-label={t.installation.title} data-installation>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{t.installation.intro}</p>
        <Field label={t.installation.name} value={name} error={fields.name} onChange={(event) => setName(event.target.value)} />
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <div className="flex items-center gap-3">
          <Button type="submit" loading={rename.isPending} data-action="rename-installation">
            {t.installation.save}
          </Button>
          <p role="status" className="text-sm text-ink-muted empty:hidden" data-installation-notice>
            {notice}
          </p>
        </div>
      </Card>
    </form>
  );
}
