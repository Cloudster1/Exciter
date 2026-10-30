import { useState, type FormEvent } from "react";
import { ASSIGNABLE_ROLES, useCreateMember, type AssignableRole, type CreatedMember } from "@/api/auth";
import { ApiError } from "@/api/client";
import { Button, Dialog, ErrorBanner, Field, Select } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { useFocusWhenRendered } from "@/lib/focus";

// Adds a person with a password, for an installation whose people do not all come through an identity provider.
// A password left out is made, and shown once in the dialog, since nothing keeps it.
export function AddMember() {
  const create = useCreateMember();
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState<AssignableRole>("member");
  const [password, setPassword] = useState("");
  const [added, setAdded] = useState<CreatedMember | null>(null);
  const [copied, setCopied] = useState(false);
  const focusLater = useFocusWhenRendered();
  const fields = create.error instanceof ApiError ? create.error.fields : {};

  function close() {
    setOpen(false);
    setAdded(null);
    setEmail("");
    setName("");
    setRole("member");
    setPassword("");
    create.reset();
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { email: email.trim(), name: name.trim(), role, password },
      {
        onSuccess: (member) => {
          setAdded(member);
          setCopied(false);
          setPassword("");
          // The form and the button that sent it are gone; the way to keep the password is what comes next.
          focusLater(() => document.querySelector<HTMLElement>('[data-action="copy-password"], [data-action="done"]'));
        },
      },
    );
  }

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <>
      <Button variant="primary" size="sm" icon={<Icon.Plus />} onClick={() => setOpen(true)} data-action="add-member">
        {t.members.add}
      </Button>
      {open && (
        <Dialog open title={added ? t.members.addedTitle : t.members.add} onClose={close} attrs={{ "data-add-member": "" }}>
          {added ? (
            <div role="status" className="space-y-3" data-member-added={added.email}>
              <p className="text-sm text-ink">
                {added.newAccount ? t.members.addedNew(added.name || added.email) : t.members.addedExisting(added.name || added.email)}
              </p>
              {added.password && (
                <div className="space-y-1">
                  <p className="text-sm text-ink-muted">{t.members.passwordOnce}</p>
                  <div className="flex flex-wrap items-center gap-2">
                    <code className="rounded-control bg-surface-sunken px-2 py-1 font-mono text-sm text-ink select-all" data-new-password="">
                      {added.password}
                    </code>
                    <Button size="sm" variant="secondary" onClick={() => copy(added.password!)} data-action="copy-password">
                      {copied ? t.members.copied : t.members.copyPassword}
                    </Button>
                  </div>
                </div>
              )}
              <div className="flex justify-end">
                <Button variant="primary" onClick={close} data-action="done">
                  {t.members.done}
                </Button>
              </div>
            </div>
          ) : (
            <form onSubmit={submit} className="space-y-3" noValidate aria-label={t.members.add}>
              <p className="text-sm text-ink-muted">{t.members.addIntro}</p>
              <Field
                label={t.members.email}
                type="email"
                autoComplete="off"
                required
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                error={fields.email}
              />
              <Field
                label={t.members.name}
                autoComplete="off"
                value={name}
                onChange={(event) => setName(event.target.value)}
                hint={t.members.nameHint}
                error={fields.name}
              />
              <Select label={t.members.role} value={role} onChange={(event) => setRole(event.target.value as AssignableRole)} error={fields.role}>
                {ASSIGNABLE_ROLES.map((each) => (
                  <option key={each} value={each}>
                    {t.members.roleNames[each]}
                  </option>
                ))}
              </Select>
              <Field
                label={t.members.password}
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                hint={t.members.passwordHint}
                error={fields.password}
              />
              {create.error && Object.keys(fields).length === 0 && <ErrorBanner>{create.error.message}</ErrorBanner>}
              <div className="flex justify-end gap-2">
                <Button type="button" variant="secondary" onClick={close}>
                  {t.kit.cancel}
                </Button>
                <Button type="submit" variant="primary" loading={create.isPending} data-action="create-member">
                  {t.members.addSubmit}
                </Button>
              </div>
            </form>
          )}
        </Dialog>
      )}
    </>
  );
}
