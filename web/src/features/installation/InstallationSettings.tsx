import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useInstallation, useRenameInstallation, type Installation } from "@/api/installation";
import { Button, Card, ErrorBanner, Field, Skeleton } from "@/components/ui";
import { t } from "@/i18n";

/** The installation's name, for administrators to change. */
export function InstallationSettings() {
  const { data, isLoading, error } = useInstallation();
  // Held here rather than in the form: a rename remounts the form, and what it
  // says about that rename has to outlive the remount.
  const rename = useRenameInstallation();
  const [notice, setNotice] = useState("");
  if (isLoading) return <Skeleton />;
  if (error || !data) return <ErrorBanner>{error?.message}</ErrorBanner>;
  return <NameForm key={data.name} installation={data} rename={rename} notice={notice} setNotice={setNotice} />;
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
