import { useState } from "react";
import { ASSIGNABLE_ROLES, useMe, useMembers, useRemoveMember, useSetMemberRole, type AssignableRole, type Member } from "@/api/auth";
import { Button, Dialog, ErrorBanner, PageHeader, SelectInput, Skeleton, Table, Td, Th } from "@/components/ui";
import { t } from "@/i18n";
import { useFocusWhenRendered } from "@/lib/focus";
import { localDateFormat } from "@/lib/format";
import { AddMember } from "./AddMember";

const joined = localDateFormat({ dateStyle: "medium" });

const nameOf = (member: Member) => member.name || member.email;

/** The organization's people and their roles: an administrator adds, re-roles and removes them here. */
export function Members() {
  const { data: me } = useMe();
  const members = useMembers();
  const setRole = useSetMemberRole();
  const remove = useRemoveMember();
  const [confirming, setConfirming] = useState<Member | null>(null);
  const [notice, setNotice] = useState("");
  const focusLater = useFocusWhenRendered();
  const list = members.data ?? [];

  function changeRole(member: Member, role: AssignableRole) {
    setNotice("");
    setRole.mutate(
      { userId: member.userId, role },
      { onSuccess: (changed) => setNotice(t.members.roleChanged(nameOf(changed), t.members.roleNames[changed.role])) },
    );
  }

  function removeConfirmed(member: Member) {
    remove.mutate(
      { userId: member.userId },
      {
        onSuccess: () => {
          setConfirming(null);
          setNotice(t.members.removed(nameOf(member)));
          // The row and its button are gone, so focus goes somewhere that stays rather than to the page's end.
          focusLater(() => document.querySelector<HTMLElement>('[data-action="add-member"]'));
        },
      },
    );
  }

  return (
    <div className="mx-auto max-w-4xl" data-members>
      <PageHeader title={t.members.title} meta={members.data ? t.members.count(list.length) : undefined} actions={<AddMember />} />
      <p className="mb-4 text-sm text-ink-muted">{t.members.intro}</p>
      {members.error && <ErrorBanner onRetry={() => void members.refetch()}>{members.error.message}</ErrorBanner>}
      {setRole.error && <ErrorBanner>{setRole.error.message}</ErrorBanner>}
      {notice && (
        <p role="status" className="mb-3 text-sm text-ink-muted" data-members-notice>
          {notice}
        </p>
      )}
      {members.isLoading ? (
        <Skeleton />
      ) : (
        list.length > 0 && (
          <Table dense aria-label={t.members.title}>
            <thead>
              <tr>
                <Th>{t.members.columnPerson}</Th>
                <Th className="w-44">{t.members.columnRole}</Th>
                <Th className="w-32">{t.members.columnJoined}</Th>
                <Th className="w-28">
                  <span className="sr-only">{t.members.remove}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {list.map((member) => {
                const self = member.userId === me?.user.id;
                const name = nameOf(member);
                const fixed = member.role === "owner" || self;
                return (
                  <tr key={member.userId} data-member={member.email}>
                    <Td className="min-w-0">
                      <div className="truncate text-ink">
                        {name}
                        {self && <span className="text-ink-subtle"> ({t.members.you})</span>}
                      </div>
                      <div className="truncate text-xs text-ink-subtle">{member.email}</div>
                    </Td>
                    <Td>
                      {fixed ? (
                        <span className="text-ink" data-member-role={member.role}>
                          {t.members.roleNames[member.role]}
                        </span>
                      ) : (
                        <SelectInput
                          aria-label={t.members.roleOf(name)}
                          controlSize="sm"
                          value={member.role}
                          disabled={setRole.isPending}
                          onChange={(event) => changeRole(member, event.target.value as AssignableRole)}
                          data-member-role={member.role}
                        >
                          {ASSIGNABLE_ROLES.map((role) => (
                            <option key={role} value={role}>
                              {t.members.roleNames[role]}
                            </option>
                          ))}
                        </SelectInput>
                      )}
                    </Td>
                    <Td className="text-sm whitespace-nowrap text-ink-muted">
                      <time dateTime={member.joinedAt}>{joined.format(new Date(member.joinedAt))}</time>
                    </Td>
                    <Td className="text-right">
                      {!fixed && (
                        <Button
                          size="sm"
                          variant="secondary"
                          onClick={() => {
                            remove.reset();
                            setConfirming(member);
                          }}
                          aria-label={t.members.removeWho(name)}
                          data-action="remove-member"
                        >
                          {t.members.remove}
                        </Button>
                      )}
                    </Td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        )
      )}
      {confirming && (
        <Dialog title={t.members.confirmTitle(nameOf(confirming))} onClose={() => setConfirming(null)} data-confirm-remove="">
          <p className="text-sm text-ink">{t.members.confirmBody}</p>
          {remove.error && (
            <div className="mt-3">
              <ErrorBanner>{remove.error.message}</ErrorBanner>
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setConfirming(null)}>
              {t.common.cancel}
            </Button>
            <Button variant="danger" onClick={() => removeConfirmed(confirming)} loading={remove.isPending} data-action="confirm-remove-member">
              {t.members.confirm}
            </Button>
          </div>
        </Dialog>
      )}
    </div>
  );
}
