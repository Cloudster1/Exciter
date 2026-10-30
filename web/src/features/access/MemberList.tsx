import { useState } from "react";
import { STANDINGS, useMe, useMembers, useSetMemberActive, useSetMemberStanding, type Member, type Standing } from "@/api/auth";
import { Button, Dialog, ErrorBanner, SelectInput, Skeleton, Table, Tag, Td, Th, countOf, useToast } from "@/components/ui";
import { AddMember } from "@/features/auth/AddMember";
import { JoinRequests } from "@/features/auth/JoinRequests";
import { t } from "@/i18n";
import { useFocusWhenRendered } from "@/lib/focus";
import { localDateFormat } from "@/lib/format";

const joined = localDateFormat({ dateStyle: "medium" });

const nameOf = (member: Member) => member.name || member.email;

/**
 * Who is in the installation, with their standing and the roles they hold
 * across it, by name or through a group. An administrator adds people, lets
 * waiting ones in, makes them members or guests and switches them off and on.
 */
export function MemberList() {
  const { data: me } = useMe();
  const members = useMembers();
  const setStanding = useSetMemberStanding();
  const setActive = useSetMemberActive();
  const [confirming, setConfirming] = useState<Member | null>(null);
  const toast = useToast();
  const focusLater = useFocusWhenRendered();
  const closeConfirm = () => {
    setActive.reset();
    setConfirming(null);
  };
  const list = members.data ?? [];

  function changeStanding(member: Member, role: Standing) {
    setStanding.mutate(
      { userId: member.userId, role },
      { onSuccess: (changed) => toast.success(t.accessAdmin.members.standingChanged(nameOf(changed), t.members.roleNames[changed.role])) },
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
    <div className="space-y-4" data-members="">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-prose text-sm text-ink-muted">{t.accessAdmin.members.intro}</p>
        <AddMember />
      </div>
      <JoinRequests />
      {members.error && <ErrorBanner onRetry={() => void members.refetch()}>{members.error.message}</ErrorBanner>}
      {setStanding.error && <ErrorBanner>{setStanding.error.message}</ErrorBanner>}
      {!confirming && setActive.error && <ErrorBanner>{setActive.error.message}</ErrorBanner>}
      {members.isLoading ? (
        <Skeleton />
      ) : (
        list.length > 0 && (
          <section className="space-y-2">
            <p className="text-sm text-ink-muted" data-members-total={list.length}>
              {countOf(list.length, t.kit.counted.member)}
            </p>
            <Table dense aria-label={t.members.title}>
              <thead>
                <tr>
                  <Th>{t.members.columnPerson}</Th>
                  <Th className="w-36">{t.accessAdmin.members.columnStanding}</Th>
                  <Th>{t.accessAdmin.members.columnRoles}</Th>
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
                        {self ? (
                          <span className="text-ink" data-member-role={member.role}>
                            {t.members.roleNames[member.role]}
                          </span>
                        ) : (
                          <SelectInput
                            aria-label={t.accessAdmin.members.standingOf(name)}
                            controlSize="sm"
                            value={member.role}
                            disabled={setStanding.isPending}
                            onChange={(event) => changeStanding(member, event.target.value as Standing)}
                            data-member-role={member.role}
                          >
                            {STANDINGS.map((standing) => (
                              <option key={standing} value={standing}>
                                {t.members.roleNames[standing]}
                              </option>
                            ))}
                          </SelectInput>
                        )}
                      </Td>
                      <Td>
                        {member.roles.length === 0 ? (
                          <span className="text-ink-subtle">{t.accessAdmin.members.noRoles}</span>
                        ) : (
                          <ul className="flex flex-wrap gap-1.5" data-member-roles>
                            {member.roles.map((role) => (
                              <li key={`${role.role}:${role.groupId ?? ""}`} data-member-holds={role.role}>
                                <Tag>
                                  {role.name}
                                  {role.groupName && (
                                    <span className="text-ink-subtle">
                                      {" "}
                                      {t.accessAdmin.members.through(role.groupBuiltin === "everyone" ? t.permissions.everyone : role.groupName)}
                                    </span>
                                  )}
                                </Tag>
                              </li>
                            ))}
                          </ul>
                        )}
                      </Td>
                      <Td className="text-sm whitespace-nowrap text-ink-muted">
                        <time dateTime={member.joinedAt}>{joined.format(new Date(member.joinedAt))}</time>
                      </Td>
                      <Td className="text-right">
                        {member.active ? (
                          !self && (
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
          </section>
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
    </div>
  );
}
