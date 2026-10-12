import { useState } from "react";
import { useAdmitJoinRequest, useDeclineJoinRequest, useJoinRequests, type JoinRequest } from "@/api/auth";
import { Button, Card, ErrorBanner } from "@/components/ui";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** People the identity provider vouched for who found no membership here; letting one in is the whole of an
 * invitation, minus the mail, as in Armature. */
export function JoinRequests() {
  const { data } = useJoinRequests();
  const admit = useAdmitJoinRequest();
  const decline = useDeclineJoinRequest();
  const [notice, setNotice] = useState("");
  const requests = data ?? [];
  const failure = admit.error ?? decline.error;
  if (requests.length === 0 && !notice) return null;

  function letIn(request: JoinRequest) {
    admit.mutate({ userId: request.userId, role: "member" }, { onSuccess: () => setNotice(t.sso.admitted(request.name || request.email)) });
  }

  function turnAway(request: JoinRequest) {
    decline.mutate({ userId: request.userId }, { onSuccess: () => setNotice(t.sso.declined(request.name || request.email)) });
  }

  return (
    <Card className="mb-4 space-y-2 p-4" data-join-requests="">
      <h2 className="text-sm font-semibold text-ink">{t.sso.waitingTitle}</h2>
      <p className="text-sm text-ink-muted">{t.sso.waitingIntro}</p>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {notice && (
        <p role="status" className="text-sm text-ink-muted">
          {notice}
        </p>
      )}
      <ul className="divide-y divide-border">
        {requests.map((request) => (
          <li key={request.userId} className="flex flex-wrap items-center gap-3 py-2" data-join-request={request.email}>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm text-ink">{request.name || request.email}</div>
              <div className="truncate text-xs text-ink-subtle">{t.sso.asked(request.email, when.format(new Date(request.requestedAt)))}</div>
            </div>
            <Button size="sm" variant="secondary" onClick={() => turnAway(request)} loading={decline.isPending} data-action="decline-request">
              {t.sso.decline}
            </Button>
            <Button size="sm" onClick={() => letIn(request)} loading={admit.isPending} data-action="admit-request">
              {t.sso.admit}
            </Button>
          </li>
        ))}
      </ul>
    </Card>
  );
}
