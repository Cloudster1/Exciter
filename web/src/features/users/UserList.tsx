import { useState } from "react";
import { useMe, useMembers, useSetMemberActive, type Member } from "@/api/auth";
import { Button, Dialog, EmptyState, ErrorBanner, Skeleton, Table, Tag, Td, Th, countOf, useToast } from "@/components/ui";
import { t } from "@/i18n";
import { useFocusWhenRendered } from "@/lib/focus";

const nameOf = (member: Member) => member.name || member.email;

/** Every account of the installation, with whether it can sign in and a way to switch it off or back on. */
export function UserList() {
  const { data: me } = useMe();
  const members = useMembers();
  const setActive = useSetMemberActive();
  const [confirming, setConfirming] = useState<Member | null>(null);
  const toast = useToast();
  const focusLater = useFocusWhenRendered();
  const closeConfirm = () => {
    setActive.reset();
    setConfirming(null);
  };
  const list = members.data ?? [];
  const u = t.admin.users;

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
    <section aria-labelledby="users-accounts" className="space-y-2" data-users="">
      <h2 id="users-accounts" className="text-sm font-semibold text-ink">
        {u.accounts}
      </h2>
      {members.error && <ErrorBanner onRetry={() => void members.refetch()}>{members.error.message}</ErrorBanner>}
      {!confirming && setActive.error && <ErrorBanner>{setActive.error.message}</ErrorBanner>}
      {members.isLoading ? (
        <Skeleton />
      ) : list.length === 0 ? (
        !members.error && <EmptyState title={u.empty} />
      ) : (
        <>
          <p className="text-sm text-ink-muted" data-users-total={list.length}>
            {countOf(list.length, t.kit.counted.member)}
          </p>
          <Table dense aria-label={u.accounts}>
            <thead>
              <tr>
                <Th>{t.members.columnPerson}</Th>
                <Th className="w-40">{u.columnState}</Th>
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
                      </div>
                      <div className="truncate text-xs text-ink-subtle">{member.email}</div>
                    </Td>
                    <Td>
                      {member.active ? <span className="text-sm text-ink-muted">{u.on}</span> : <Tag data-switched-off="">{t.members.switchedOffTag}</Tag>}
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
        </>
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
    </section>
  );
}
