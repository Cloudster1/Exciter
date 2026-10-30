import { useState, type FormEvent } from "react";
import { groupLabel, useAddToGroup, useCreateGroup, useDeleteGroup, useGroup, useGroups, useRemoveFromGroup, useUpdateGroup, type Group } from "@/api/access";
import { Button, Card, Dialog, EmptyState, ErrorBanner, Field, Skeleton, Tag, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { SubjectPicker } from "@/features/permissions/SubjectPicker";
import { t } from "@/i18n";

/**
 * Groups, and who is in them. A group whose members come from the identity
 * provider is shown but not filled here: a change would last until the
 * person's next sign-in, which is worse than not offering it.
 */
export function GroupList() {
  const { data, isLoading, error } = useGroups();
  const groups = data ?? [];
  if (error) return <ErrorBanner>{error.message}</ErrorBanner>;
  return (
    <div className="space-y-4" data-group-list>
      <p className="max-w-prose text-sm text-ink-muted">{t.accessAdmin.groups.intro}</p>
      <NewGroup />
      {isLoading && <Skeleton lines={4} />}
      {!isLoading && groups.every((group) => group.builtin) && <EmptyState title={t.accessAdmin.groups.empty} />}
      <div className="space-y-3">
        {groups.map((group) => (group.builtin ? <BuiltinGroupCard key={group.id} group={group} /> : <GroupCard key={group.id} group={group} />))}
      </div>
    </div>
  );
}

/** Everyone, whose members are the installation's: counted, since it holds every member, and never edited here. */
function BuiltinGroupCard({ group }: { group: Group }) {
  return (
    <Card className="p-4" data-group={groupLabel(group)} data-group-builtin={group.builtin}>
      <h2 className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold text-ink">
        <span className="break-words">{groupLabel(group)}</span>
        <Tag>{t.accessAdmin.groups.builtin}</Tag>
      </h2>
      <p className="mt-0.5 text-xs text-ink-muted">
        {t.accessAdmin.groups.people(group.memberCount)} · {t.accessAdmin.groups.roles(group.roleCount)}
      </p>
      <p className="mt-2 text-sm text-ink-muted">{t.accessAdmin.groups.everyoneDetail}</p>
    </Card>
  );
}

function GroupCard({ group }: { group: Group }) {
  const { data } = useGroup(group.id);
  const add = useAddToGroup();
  const remove = useRemoveFromGroup();
  const deleteGroup = useDeleteGroup();
  const confirm = useConfirm();
  const toast = useToast();
  const [editing, setEditing] = useState(false);
  const members = data?.members ?? [];
  const fromProvider = group.source === "oidc";
  const failure = (add.error ?? remove.error ?? deleteGroup.error) as Error | null;

  async function drop() {
    if (
      await confirm({
        noun: t.kit.confirm.things.named(t.kit.confirm.things.group, group.name),
        body: t.accessAdmin.groups.deleteBody(group.name, group.roleCount),
      })
    ) {
      // The card leaves the list before a callback of its own could run, so
      // the toast waits on the promise, which outlives it.
      await deleteGroup.mutateAsync(group.id).then(
        () => toast.success(t.accessAdmin.groups.deleted(group.name)),
        () => undefined,
      );
    }
  }

  async function takeOut(userId: string, name: string) {
    if (
      await confirm({
        noun: t.kit.confirm.things.member,
        verb: t.kit.confirm.verbs.remove,
        body: t.accessAdmin.groups.removeBody(name, group.name),
      })
    ) {
      remove.mutate({ id: group.id, userId }, { onSuccess: () => toast.success(t.accessAdmin.groups.removed(name, group.name)) });
    }
  }

  return (
    <Card className="p-4" data-group={group.name} data-group-source={group.source}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold text-ink">
            <span className="break-words">{group.name}</span>
            {fromProvider && <Tag>{t.accessAdmin.groups.fromProvider}</Tag>}
          </h2>
          <p className="mt-0.5 text-xs text-ink-muted">
            {t.accessAdmin.groups.people(group.memberCount)} · {t.accessAdmin.groups.roles(group.roleCount)}
            {group.externalRef && ` · ${t.accessAdmin.groups.matches(group.externalRef)}`}
          </p>
          {group.description && <p className="mt-1 text-sm text-ink-muted">{group.description}</p>}
        </div>
        <div className="flex shrink-0 gap-1">
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)} data-action="edit-group">
            {t.accessAdmin.groups.edit}
          </Button>
          <Button size="sm" variant="ghost" loading={deleteGroup.isPending} onClick={() => void drop()} data-action="delete-group">
            {t.accessAdmin.groups.delete}
          </Button>
        </div>
      </div>

      <ul className="mt-2 space-y-1 text-sm" data-group-members>
        {members.map((member) => {
          const name = member.name || member.email;
          return (
            <li key={member.id} className="flex items-center gap-2" data-group-member={name}>
              <span className="min-w-0 truncate text-ink">{name}</span>
              {!fromProvider && (
                <Button
                  size="sm"
                  variant="ghost"
                  className="ml-auto"
                  onClick={() => void takeOut(member.id, name)}
                  aria-label={t.accessAdmin.groups.removeWho(name)}
                  data-action="remove-group-member"
                >
                  {t.accessAdmin.groups.remove}
                </Button>
              )}
            </li>
          );
        })}
        {members.length === 0 && (
          <li className="text-sm text-ink-subtle">{fromProvider ? t.accessAdmin.groups.nobodyFromProvider : t.accessAdmin.groups.nobody}</li>
        )}
      </ul>

      {!fromProvider && (
        <div className="mt-3">
          <SubjectPicker
            label={t.accessAdmin.groups.addTo(group.name)}
            peopleOnly
            exclude={members.map((member) => `user:${member.id}`)}
            onPick={(person) => {
              if (!person.id) return;
              add.mutate({ id: group.id, userId: person.id }, { onSuccess: () => toast.success(t.accessAdmin.groups.added(person.name, group.name)) });
            }}
          />
        </div>
      )}
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {editing && <EditGroupDialog group={group} onClose={() => setEditing(false)} />}
    </Card>
  );
}

function NewGroup() {
  const create = useCreateGroup();
  const toast = useToast();
  const [name, setName] = useState("");
  const [ref, setRef] = useState("");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), externalRef: ref.trim() || undefined },
      {
        onSuccess: (made) => {
          toast.success(t.accessAdmin.groups.created(made.name));
          setName("");
          setRef("");
        },
      },
    );
  }

  return (
    <Card className="p-4">
      <form onSubmit={onSubmit} className="space-y-3" aria-label={t.accessAdmin.groups.newGroup}>
        <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
          <Field
            label={t.accessAdmin.groups.newGroup}
            value={name}
            placeholder={t.accessAdmin.groups.namePlaceholder}
            onChange={(event) => setName(event.target.value)}
          />
          <Field label={t.accessAdmin.groups.claim} value={ref} placeholder={t.accessAdmin.groups.optional} onChange={(event) => setRef(event.target.value)} />
          <Button type="submit" loading={create.isPending} disabled={!name.trim()} data-action="create-group">
            {t.accessAdmin.groups.create}
          </Button>
        </div>
        <p className="text-xs text-ink-subtle">{t.accessAdmin.groups.claimHint}</p>
        {create.error && <ErrorBanner>{create.error.message}</ErrorBanner>}
      </form>
    </Card>
  );
}

function EditGroupDialog({ group, onClose }: { group: Group; onClose: () => void }) {
  const update = useUpdateGroup();
  const toast = useToast();
  const [name, setName] = useState(group.name);
  const [description, setDescription] = useState(group.description);
  const [ref, setRef] = useState(group.externalRef);
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    update.mutate(
      { id: group.id, name: name.trim(), description: description.trim(), externalRef: ref.trim() },
      {
        onSuccess: (saved) => {
          toast.success(t.accessAdmin.groups.saved(saved.name));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog open onClose={onClose} title={t.accessAdmin.groups.editTitle(group.name)} attrs={{ "data-edit-group": group.name }}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Field label={t.accessAdmin.matrix.name} id="field-group-name" autoFocus value={name} onChange={(event) => setName(event.target.value)} />
        <Field label={t.accessAdmin.matrix.purpose} id="field-group-description" value={description} onChange={(event) => setDescription(event.target.value)} />
        <Field
          label={t.accessAdmin.groups.claim}
          id="field-group-claim"
          value={ref}
          hint={t.accessAdmin.groups.claimHint}
          onChange={(event) => setRef(event.target.value)}
        />
        {update.error && <ErrorBanner>{update.error.message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()} data-action="save-group">
            {t.accessAdmin.groups.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
