import { useState } from "react";
import { ASSIGNABLE_ROLES, useMe, useMembers, useSetMemberActive, useSetMemberRole, type AssignableRole, type Member } from "@/api/auth";
import { Button, Dialog, ErrorBanner, PageHeader, SelectInput, Skeleton, Table, Tag, Td, Th, useToast, Page } from "@/components/ui";
import { t } from "@/i18n";
import { useFocusWhenRendered } from "@/lib/focus";
import { localDateFormat } from "@/lib/format";
import { AddMember } from "./AddMember";
import { JoinRequests } from "./JoinRequests";

const joined = localDateFormat({ dateStyle: "medium" });

const nameOf = (member: Member) => member.name || member.email;

/** The installation's people and their roles: an administrator adds, re-roles and switches them off and on here. */
export function Members() {
  const { data: me } = useMe();
  const members = useMembers();
  const setRole = useSetMemberRole();
  const setActive = useSetMemberActive();
  const [confirming, setConfirming] = useState<Member | null>(null);
  const toast = useToast();
  const focusLater = useFocusWhenRendered();
  const closeConfirm = () => {
    setActive.reset();
    setConfirming(null);
  };
  const list = members.data ?? [];

  function changeRole(member: Member, role: AssignableRole) {
    setRole.mutate(
      { userId: member.userId, role },
      { onSuccess: (changed) => toast.success(t.members.roleChanged(nameOf(changed), t.members.roleNames[changed.role])) },
    );
  }

  function switchOffConfirmed(member: Member) {
    setActive.mutate(
      { userId: member.userId, active: false },
      {
        onSuccess: () => {
          setConfirming(null);
          toast.success(t.members.switchedOff(nameOf(member)));
          // The row moves to the end of the list with another button, so focus goes somewhere that stays put.
          focusLater(() => document.querySelector<HTMLElement>('[data-action="add-member"]'));
        },
      },
    );
  }

  function switchOn(member: Member) {
    setActive.mutate({ userId: member.userId, active: true }, { onSuccess: () => toast.success(t.members.switchedOn(nameOf(member))) });
  }

  return (
    <Page width="content" data-members>
      <PageHeader title={t.members.title} meta={members.data ? t.members.count(list.length) : undefined} actions={<AddMember />} />
      <p className="mb-4 text-sm text-ink-muted">{t.members.intro}</p>
      <JoinRequests />
      {members.error && <ErrorBanner onRetry={() => void members.refetch()}>{members.error.message}</ErrorBanner>}
      {setRole.error && <ErrorBanner>{setRole.error.message}</ErrorBanner>}
      {!confirming && setActive.error && <ErrorBanner>{setActive.error.message}</ErrorBanner>}
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
                  <span className="sr-only">{t.members.switchOff}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {list.map((member) => {
                const self = member.userId === me?.user.id;
                const name = nameOf(member);
                const fixed = member.role === "owner" || self;
                // Only an owner lets another owner go, and nobody themselves.
                const mayGo = !self && (member.role !== "owner" || me?.role === "owner");
                return (
                  <tr key={member.userId} data-member={member.email} data-member-active={member.active}>
                    <Td className="min-w-0">
                      <div className="truncate text-ink">
                        {name}
                        {self && <span className="text-ink-subtle"> ({t.members.you})</span>}
                        {!member.active && (
                          <Tag className="ml-2" data-switched-off="">
                            {t.members.switchedOffTag}
                          </Tag>
                        )}
                      </div>
                      <div className="truncate text-xs text-ink-subtle">{member.email}</div>
                    </Td>
                    <Td>
                      {member.roleSource === "oidc" && (
                        <div className="mb-1 text-xs text-ink-subtle" data-role-source="oidc">
                          {t.members.fromProvider}
                        </div>
                      )}
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
                      {member.active ? (
                        mayGo && (
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => {
                              setActive.reset();
                              setConfirming(member);
                            }}
                            aria-label={t.members.switchOffWho(name)}
                            data-action="switch-off-member"
                          >
                            {t.members.switchOff}
                          </Button>
                        )
                      ) : (
                        <Button
                          size="sm"
                          variant="secondary"
                          onClick={() => switchOn(member)}
                          disabled={setActive.isPending}
                          aria-label={t.members.switchOnWho(name)}
                          data-action="switch-on-member"
                        >
                          {t.members.switchOn}
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
        <Dialog
          open
          title={t.members.confirmTitle(nameOf(confirming))}
          onClose={closeConfirm}
          attrs={{ "data-confirm-switch-off": "" }}
          footer={
            <>
              <Button variant="secondary" onClick={closeConfirm}>
                {t.kit.cancel}
              </Button>
              <Button variant="danger" onClick={() => switchOffConfirmed(confirming)} loading={setActive.isPending} data-action="confirm-switch-off-member">
                {t.members.confirm}
              </Button>
            </>
          }
        >
          <p className="text-sm text-ink">{t.members.confirmBody}</p>
          {setActive.error && (
            <div className="mt-3">
              <ErrorBanner>{setActive.error.message}</ErrorBanner>
            </div>
          )}
        </Dialog>
      )}
    </Page>
  );
}
