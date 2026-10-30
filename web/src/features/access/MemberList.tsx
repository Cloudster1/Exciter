import { STANDINGS, useMe, useMembers, useSetMemberStanding, type Member, type Standing } from "@/api/auth";
import { ErrorBanner, SelectInput, Skeleton, Table, Tag, Td, Th, countOf, useToast } from "@/components/ui";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const joined = localDateFormat({ dateStyle: "medium" });

const nameOf = (member: Member) => member.name || member.email;

/**
 * Who is in the installation, with their standing and the roles they hold
 * across it, by name or through a group. An administrator makes them members
 * or guests here; adding, letting in and switching off are under Users.
 */
export function MemberList() {
  const { data: me } = useMe();
  const members = useMembers();
  const setStanding = useSetMemberStanding();
  const toast = useToast();
  const list = members.data ?? [];

  function changeStanding(member: Member, role: Standing) {
    setStanding.mutate(
      { userId: member.userId, role },
      { onSuccess: (changed) => toast.success(t.accessAdmin.members.standingChanged(nameOf(changed), t.members.roleNames[changed.role])) },
    );
  }

  return (
    <div className="space-y-4" data-members="">
      <p className="max-w-prose text-sm text-ink-muted">{t.accessAdmin.members.intro}</p>
      {members.error && <ErrorBanner onRetry={() => void members.refetch()}>{members.error.message}</ErrorBanner>}
      {setStanding.error && <ErrorBanner>{setStanding.error.message}</ErrorBanner>}
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
                    </tr>
                  );
                })}
              </tbody>
            </Table>
          </section>
        )
      )}
    </div>
  );
}
