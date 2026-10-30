import { useState, type FormEvent } from "react";
import {
  ADMINISTRATOR_ROLE,
  permissionWords,
  useCreateRole,
  useDeleteRole,
  usePermissions,
  useRoles,
  useUpdateRole,
  type Permission,
  type PermissionDescription,
  type RoleDescription,
} from "@/api/access";
import { Button, Checkbox, Dialog, ErrorBanner, Field, IconButton, Menu, Skeleton, Switch, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

/** The one tick that cannot come off: without it nobody could put it back. */
function fixed(role: RoleDescription, permission: Permission): boolean {
  return role.role === ADMINISTRATOR_ROLE && permission === "administerInstallation";
}

/**
 * What each role holds, as a grid: permissions down, roles across, a box at
 * each crossing. A box is saved the moment it is ticked, and everybody who
 * holds the role may do it at once. The built-in roles can be changed but not
 * removed; a role an administrator added can go, with its grants.
 */
export function RoleMatrix() {
  const { data: roles, isLoading, error } = useRoles();
  const { data: permissions } = usePermissions();
  const update = useUpdateRole();
  const remove = useDeleteRole();
  const confirm = useConfirm();
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const [renaming, setRenaming] = useState<RoleDescription | null>(null);
  const failure = (error ?? update.error ?? remove.error) as Error | null;

  function toggle(role: RoleDescription, permission: PermissionDescription, on: boolean) {
    const next = on ? [...role.permissions, permission.permission] : role.permissions.filter((p) => p !== permission.permission);
    const words = permissionWords(permission);
    update.mutate(
      { role: role.role, permissions: next },
      { onSuccess: () => toast.success(on ? t.accessAdmin.matrix.ticked(role.name, words) : t.accessAdmin.matrix.unticked(role.name, words)) },
    );
  }

  async function drop(role: RoleDescription) {
    if (
      await confirm({ noun: t.kit.confirm.things.named(t.kit.confirm.things.role, role.name), body: t.accessAdmin.matrix.deleteBody(role.name, role.inUse) })
    ) {
      remove.mutate(role.role, { onSuccess: () => toast.success(t.accessAdmin.matrix.deleted(role.name)) });
    }
  }

  if (isLoading || !roles || !permissions) return <Skeleton lines={6} />;

  const sections = [
    { id: "project", title: t.accessAdmin.matrix.project, detail: t.accessAdmin.matrix.projectDetail, rows: permissions.filter((p) => p.scope === "project") },
    {
      id: "installation",
      title: t.accessAdmin.matrix.installation,
      detail: t.accessAdmin.matrix.installationDetail,
      rows: permissions.filter((p) => p.scope === "installation"),
    },
  ];

  return (
    <div className="space-y-4" data-role-matrix>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-prose text-sm text-ink-muted">{t.accessAdmin.matrix.intro}</p>
        <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-role">
          {t.accessAdmin.matrix.newRole}
        </Button>
      </div>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <div className="overflow-x-auto">
        <Table dense aria-label={t.accessAdmin.tabs.matrix}>
          <thead>
            <tr>
              <Th>{t.accessAdmin.matrix.permission}</Th>
              {roles.map((role) => (
                <Th key={role.role} className="text-center align-top" data-role-column={role.role}>
                  <span className="inline-flex flex-col items-center gap-1 normal-case tracking-normal">
                    <span className="text-xs font-medium whitespace-nowrap text-ink" title={role.description}>
                      {role.name}
                    </span>
                    <span className="inline-flex items-center gap-1">
                      {role.builtin && <Tag className="whitespace-nowrap">{t.accessAdmin.matrix.builtIn}</Tag>}
                      {role.orgWideOnly && <Tag className="whitespace-nowrap">{t.accessAdmin.matrix.wholeOnly}</Tag>}
                      <Menu
                        label={t.accessAdmin.matrix.actionsFor(role.name)}
                        align="end"
                        trigger={(props) => (
                          <IconButton
                            icon={<Icon.More />}
                            label={t.accessAdmin.matrix.actionsFor(role.name)}
                            size="xs"
                            onClick={props.toggle}
                            aria-haspopup={props["aria-haspopup"]}
                            aria-expanded={props["aria-expanded"]}
                            data-action="role-menu"
                          />
                        )}
                        items={[
                          {
                            label: t.accessAdmin.matrix.rename,
                            icon: <Icon.Edit />,
                            onSelect: () => setRenaming(role),
                            attrs: { "data-action": "rename-role" },
                          },
                          {
                            label: t.accessAdmin.matrix.delete,
                            icon: <Icon.Trash />,
                            danger: true,
                            disabled: role.builtin,
                            onSelect: () => void drop(role),
                            attrs: { "data-action": "delete-role" },
                          },
                        ]}
                      />
                    </span>
                  </span>
                </Th>
              ))}
            </tr>
          </thead>
          {sections.map((section) => (
            <tbody key={section.id} data-permission-section={section.id}>
              <tr>
                <Td colSpan={roles.length + 1} className="bg-surface-raised">
                  <span className="text-xs font-semibold text-ink">{section.title}</span>
                  <span className="ml-2 text-xs text-ink-subtle">{section.detail}</span>
                </Td>
              </tr>
              {section.rows.map((permission) => {
                const words = permissionWords(permission);
                return (
                  <tr key={permission.permission} data-permission-row={permission.permission}>
                    <Td>
                      <span className="text-ink">{words}</span>
                    </Td>
                    {roles.map((role) => {
                      const held = role.permissions.includes(permission.permission);
                      const locked = fixed(role, permission.permission);
                      return (
                        <Td key={role.role} className="text-center">
                          <Checkbox
                            label={<span className="sr-only">{t.accessAdmin.matrix.may(role.name, words)}</span>}
                            checked={held}
                            disabled={locked || update.isPending}
                            title={locked ? t.accessAdmin.matrix.fixed : undefined}
                            onChange={(event) => toggle(role, permission, event.target.checked)}
                            data-permission-cell={`${role.role}:${permission.permission}`}
                          />
                        </Td>
                      );
                    })}
                  </tr>
                );
              })}
            </tbody>
          ))}
        </Table>
      </div>
      {adding && <NewRoleDialog onClose={() => setAdding(false)} />}
      {renaming && <RenameRoleDialog role={renaming} onClose={() => setRenaming(null)} />}
    </div>
  );
}

/** Names a role of the installation's own, which starts holding viewing a project or, across the installation, nothing. */
export function NewRoleDialog({ onClose }: { onClose: () => void }) {
  const create = useCreateRole();
  const toast = useToast();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [wholeOnly, setWholeOnly] = useState(false);
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), description: description.trim(), orgWideOnly: wholeOnly, permissions: wholeOnly ? [] : ["view"] },
      {
        onSuccess: (made) => {
          toast.success(t.accessAdmin.matrix.added(made.name));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog open onClose={onClose} title={t.accessAdmin.matrix.newRole} attrs={{ "data-new-role": "" }}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Field label={t.accessAdmin.matrix.name} id="field-role-name" autoFocus value={name} onChange={(event) => setName(event.target.value)} />
        <Field label={t.accessAdmin.matrix.purpose} id="field-role-description" value={description} onChange={(event) => setDescription(event.target.value)} />
        <div className="flex items-start gap-2 text-sm text-ink-muted">
          <Switch checked={wholeOnly} onChange={setWholeOnly} label={t.accessAdmin.matrix.wholeOnlyLabel} data-role-whole-only={wholeOnly ? "true" : "false"} />
          <span>
            <span className="block text-ink" aria-hidden="true">
              {t.accessAdmin.matrix.wholeOnlyLabel}
            </span>
            {t.accessAdmin.matrix.wholeOnlyDetail}
          </span>
        </div>
        {!wholeOnly && <p className="text-xs text-ink-subtle">{t.accessAdmin.matrix.startsWith}</p>}
        {create.error && <ErrorBanner>{create.error.message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={create.isPending} disabled={!name.trim()} data-action="add-role">
            {t.accessAdmin.matrix.addRole}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function RenameRoleDialog({ role, onClose }: { role: RoleDescription; onClose: () => void }) {
  const update = useUpdateRole();
  const toast = useToast();
  const [name, setName] = useState(role.name);
  const [description, setDescription] = useState(role.description);
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    update.mutate(
      { role: role.role, name: name.trim(), description: description.trim() },
      {
        onSuccess: (saved) => {
          toast.success(t.accessAdmin.matrix.saved(saved.name));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog open onClose={onClose} title={t.accessAdmin.matrix.renameTitle(role.name)} attrs={{ "data-rename-role": role.role }}>
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Field label={t.accessAdmin.matrix.name} id="field-role-name" autoFocus value={name} onChange={(event) => setName(event.target.value)} />
        <Field label={t.accessAdmin.matrix.purpose} id="field-role-description" value={description} onChange={(event) => setDescription(event.target.value)} />
        {update.error && <ErrorBanner>{update.error.message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()} data-action="save-role">
            {t.accessAdmin.matrix.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
