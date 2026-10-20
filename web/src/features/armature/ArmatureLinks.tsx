import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import {
  useArmatureAccount,
  useArmatureIssues,
  useArmatureLinkActions,
  useArmatureLinks,
  type ArmatureIssue,
  type ArmatureLink,
  type ArmatureRelation,
} from "@/api/armature";
import type { Requirement } from "@/api/requirements";
import { Button, Card, ErrorBanner, IconButton, SectionTitle, Select, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ARMATURE_ACCOUNT_PATH, ARMATURE_RELATIONS } from "@/config";
import { t } from "@/i18n";
import { IssueChip, chipState } from "./IssueChip";
import { IssuePicker } from "./IssuePicker";
import { ClearDialog, SuspectBadge } from "@/features/links/Suspect";

/** Where an issue opens in the connected Armature. */
export function issueUrl(baseUrl: string, key: string): string {
  return `${baseUrl.replace(/\/$/, "")}/issues/${encodeURIComponent(key)}`;
}

/** The Armature issues a requirement is traced to, by relation, each read live as the viewer and with whether
 * Armature shows the requirement yet; its editors trace it to more and remove traces. */
export function ArmatureLinks({ requirement }: { requirement: Requirement }) {
  const account = useArmatureAccount();
  const configured = Boolean(account.data?.configured && account.data.baseUrl);
  const section = requirement.kind === "section";
  const { data: links, error, refetch } = useArmatureLinks(requirement.key, configured && !section);
  const connected = Boolean(account.data?.connected && account.data.status !== "rejected");
  const lookup = useArmatureIssues(
    (links ?? []).map((link) => link.issueKey),
    connected && (links?.length ?? 0) > 0,
  );
  const { remove, clear } = useArmatureLinkActions(requirement.key);
  const [notice, setNotice] = useState("");
  const [checking, setChecking] = useState<ArmatureLink | null>(null);
  if (!configured || section) return null;
  const baseUrl = account.data!.baseUrl;
  const mayLink = requirement.can.link && !requirement.trashedAt;
  const l = t.armature.links;

  function unlink(link: ArmatureLink) {
    remove.mutate(link, { onSuccess: () => setNotice(l.removed(link.issueKey)) });
  }

  return (
    <Card className="space-y-3 p-4" data-armature-links>
      <section aria-labelledby="armature-links-title" className="space-y-3">
        <SectionTitle id="armature-links-title">{l.title}</SectionTitle>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!links && !error && <Skeleton />}
        {links && links.length === 0 && <p className="text-sm text-ink-subtle">{l.none}</p>}
        {links &&
          ARMATURE_RELATIONS.map((relation) => {
            const group = links.filter((link) => link.relation === relation);
            if (group.length === 0) return null;
            return (
              <div key={relation} className="space-y-1" data-armature-relation={relation}>
                <h3 className="text-sm font-medium text-ink">{l.headings[relation]}</h3>
                <ul className="space-y-2">
                  {group.map((link) => (
                    <li key={link.id} className="space-y-0.5 text-sm" data-armature-link={link.issueKey} data-sync={link.sync}>
                      <div className="flex flex-wrap items-center gap-2">
                        <IssueChip
                          issueKey={link.issueKey}
                          href={issueUrl(baseUrl, link.issueKey)}
                          state={chipState(link.issueKey, lookup, connected)}
                          storedCategory={link.statusCategory}
                          stale={link.statusStale}
                        />
                        {link.brokenAt && <Tag>{l.broken}</Tag>}
                        {link.detachedAt && <Tag>{l.detached}</Tag>}
                        {!link.brokenAt && !link.detachedAt && link.sync !== "synced" && <Tag>{l.states[link.sync]}</Tag>}
                        {link.suspect && <SuspectBadge />}
                        {link.suspect && mayLink && (
                          <Button
                            variant="link"
                            onClick={() => {
                              clear.reset();
                              setChecking(link);
                            }}
                            aria-label={t.suspect.armatureLabel(link.issueKey)}
                            data-action="check-armature-link"
                          >
                            {t.suspect.check}
                          </Button>
                        )}
                        {mayLink && (
                          <IconButton
                            icon={<Icon.X className="size-3" />}
                            label={l.remove(link.issueKey, l.relations[link.relation])}
                            size="xs"
                            onClick={() => unlink(link)}
                            data-action="remove-armature-link"
                          />
                        )}
                      </div>
                      {link.sync === "failed" && link.error && <p className="text-xs text-ink-muted">{link.error}</p>}
                    </li>
                  ))}
                </ul>
              </div>
            );
          })}
        {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
        {mayLink && <AddIssueLink requirement={requirement} connected={connected} onLinked={(key) => setNotice(l.added(key))} />}
        <p role="status" className="sr-only" data-armature-link-notice>
          {notice}
        </p>
        {checking && (
          <ClearDialog
            title={t.suspect.armatureTitle(requirement.key, checking.issueKey)}
            intro={t.suspect.armatureIntro}
            changes={[{ reqKey: requirement.key, from: checking.reviewedRevision, to: requirement.revision }]}
            pending={clear.isPending}
            error={clear.error}
            onClose={() => setChecking(null)}
            onConfirm={(comment) =>
              clear.mutate(
                { id: checking.id, revision: requirement.revision, comment },
                {
                  onSuccess: () => {
                    setNotice(t.suspect.armatureCleared(checking.issueKey));
                    setChecking(null);
                  },
                },
              )
            }
          />
        )}
      </section>
    </Card>
  );
}

/** Picks how the issue relates and the issue, found in Armature as the viewer, and traces the requirement to it. */
function AddIssueLink({ requirement, connected, onLinked }: { requirement: Requirement; connected: boolean; onLinked: (key: string) => void }) {
  const l = t.armature.links;
  const { create } = useArmatureLinkActions(requirement.key);
  const [relation, setRelation] = useState<ArmatureRelation>("implements");
  const [issue, setIssue] = useState<ArmatureIssue | null>(null);
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const refusal = create.error ? (fields.issueKey ?? fields.relation ?? create.error.message) : "";

  if (!connected) {
    return (
      <p className="border-t border-border pt-3 text-sm text-ink-muted" data-armature-connect>
        {l.connectFirst}{" "}
        <a href={ARMATURE_ACCOUNT_PATH} className="underline">
          {l.connectLink}
        </a>
      </p>
    );
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!issue) return;
    create.mutate(
      { issueKey: issue.key, relation },
      {
        onSuccess: (link) => {
          setIssue(null);
          onLinked(link.issueKey);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="space-y-3 border-t border-border pt-3" aria-label={l.add} data-add-armature-link>
      <div className="grid gap-3 sm:grid-cols-2">
        <Select
          id="armature-link-relation"
          label={l.relation}
          value={relation}
          onChange={(event) => setRelation(event.target.value as ArmatureRelation)}
          data-armature-relation-select
        >
          {ARMATURE_RELATIONS.map((each) => (
            <option key={each} value={each}>
              {l.relations[each]}
            </option>
          ))}
        </Select>
        {issue ? (
          <div className="space-y-1">
            <span className="block text-sm font-medium text-ink-muted">{l.issue}</span>
            <p className="flex items-center gap-2 text-sm text-ink" data-armature-picked={issue.key}>
              <span className="min-w-0 truncate">{l.picked(issue.key, issue.summary)}</span>
              <Button variant="link" onClick={() => setIssue(null)} data-action="pick-another-issue">
                {l.change}
              </Button>
            </p>
          </div>
        ) : (
          <IssuePicker label={l.issue} onPick={setIssue} />
        )}
      </div>
      {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
      <Button type="submit" variant="secondary" icon={<Icon.Link />} loading={create.isPending} disabled={!issue} data-action="create-armature-link">
        {l.create}
      </Button>
    </form>
  );
}
