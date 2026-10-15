import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import {
  useStatorAccount,
  useStatorLinkActions,
  useStatorLinks,
  useStatorPages,
  type StatorLink,
  type StatorPageHit,
  type StatorPages,
  type StatorRelation,
} from "@/api/stator";
import type { Requirement } from "@/api/requirements";
import { Button, Card, ErrorBanner, IconButton, SectionTitle, Select, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { STATOR_ACCOUNT_PATH, STATOR_RELATIONS } from "@/config";
import { t } from "@/i18n";
import { ClearDialog, SuspectBadge } from "@/features/links/Suspect";
import { PageChip, pageChipState } from "./PageChip";
import { PagePicker } from "./PagePicker";

/** What a page is called to this viewer: its title when they may read it, its space otherwise. */
function pageName(link: StatorLink, lookup: StatorPages): string {
  return lookup.pages.get(link.pageId)?.title ?? `${link.spaceKey} ${t.stator.chip.page}`;
}

/** The Stator pages a requirement is traced to, by relation, each read live as the viewer; its editors trace it to
 * more, remove traces, and confirm one whose page moved on. */
export function StatorLinks({ requirement }: { requirement: Requirement }) {
  const account = useStatorAccount();
  const configured = Boolean(account.data?.configured && account.data.baseUrl);
  const section = requirement.kind === "section";
  const { data: links, error, refetch } = useStatorLinks(requirement.key, configured && !section);
  const connected = Boolean(account.data?.connected && account.data.status !== "rejected");
  const lookup = useStatorPages(
    (links ?? []).map((link) => link.pageId),
    connected && (links?.length ?? 0) > 0,
  );
  const { remove, clear } = useStatorLinkActions(requirement.key);
  const [notice, setNotice] = useState("");
  const [checking, setChecking] = useState<StatorLink | null>(null);
  if (!configured || section) return null;
  const mayLink = requirement.can.link && !requirement.trashedAt;
  const l = t.stator.links;

  function unlink(link: StatorLink) {
    const name = pageName(link, lookup);
    remove.mutate(link, { onSuccess: () => setNotice(l.removed(name)) });
  }

  return (
    <Card className="space-y-3 p-4" data-stator-links>
      <section aria-labelledby="stator-links-title" className="space-y-3">
        <SectionTitle id="stator-links-title">{l.title}</SectionTitle>
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!links && !error && <Skeleton />}
        {links && links.length === 0 && <p className="text-sm text-ink-subtle">{l.none}</p>}
        {links &&
          STATOR_RELATIONS.map((relation) => {
            const group = links.filter((link) => link.relation === relation);
            if (group.length === 0) return null;
            return (
              <div key={relation} className="space-y-1" data-stator-relation={relation}>
                <h3 className="text-sm font-medium text-ink">{l.headings[relation]}</h3>
                <ul className="space-y-2">
                  {group.map((link) => (
                    <li key={link.id} className="flex flex-wrap items-center gap-2 text-sm" data-stator-link={link.pageId}>
                      <PageChip
                        pageId={link.pageId}
                        href={link.url}
                        spaceKey={link.spaceKey}
                        version={link.pageVersion}
                        state={pageChipState(link.pageId, lookup, connected)}
                      />
                      {link.brokenAt && <Tag>{l.broken}</Tag>}
                      {link.detachedAt && <Tag>{l.detached}</Tag>}
                      {link.suspect && <SuspectBadge hint={t.suspect.statorHint} />}
                      {link.suspect && mayLink && (
                        <Button
                          variant="link"
                          onClick={() => {
                            clear.reset();
                            setChecking(link);
                          }}
                          aria-label={t.suspect.statorLabel(pageName(link, lookup))}
                          data-action="check-stator-link"
                        >
                          {t.suspect.check}
                        </Button>
                      )}
                      {mayLink && (
                        <IconButton
                          icon={<Icon.X className="size-3" />}
                          label={l.remove(pageName(link, lookup), l.relations[link.relation])}
                          size="xs"
                          onClick={() => unlink(link)}
                          data-action="remove-stator-link"
                        />
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            );
          })}
        {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
        {mayLink && <AddPageLink requirement={requirement} connected={connected} onLinked={(title) => setNotice(l.added(title))} />}
        <p role="status" className="sr-only" data-stator-link-notice>
          {notice}
        </p>
        {checking && (
          <ClearDialog
            title={t.suspect.statorTitle(requirement.key, pageName(checking, lookup))}
            intro={t.suspect.statorIntro(checking.pageVersionSeen, checking.pageVersion)}
            changes={[]}
            pending={clear.isPending}
            error={clear.error}
            onClose={() => setChecking(null)}
            onConfirm={(comment) =>
              clear.mutate(
                { id: checking.id, pageVersion: checking.pageVersion, comment },
                {
                  onSuccess: () => {
                    setNotice(t.suspect.statorCleared(pageName(checking, lookup)));
                    setChecking(null);
                  },
                },
              )
            }
          >
            <a href={checking.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-sm text-accent hover:underline">
              {t.suspect.statorOpen}
              <Icon.External aria-hidden="true" />
            </a>
          </ClearDialog>
        )}
      </section>
    </Card>
  );
}

/** Picks how the page relates and the page, found in Stator as the viewer, and traces the requirement to it. */
function AddPageLink({ requirement, connected, onLinked }: { requirement: Requirement; connected: boolean; onLinked: (title: string) => void }) {
  const l = t.stator.links;
  const { create } = useStatorLinkActions(requirement.key);
  const [relation, setRelation] = useState<StatorRelation>("specifies");
  const [page, setPage] = useState<StatorPageHit | null>(null);
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const refusal = create.error ? (fields.pageId ?? fields.relation ?? create.error.message) : "";

  if (!connected) {
    return (
      <p className="border-t border-border pt-3 text-sm text-ink-muted" data-stator-connect>
        {l.connectFirst}{" "}
        <a href={STATOR_ACCOUNT_PATH} className="underline">
          {l.connectLink}
        </a>
      </p>
    );
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!page) return;
    const title = page.title;
    create.mutate(
      { pageId: page.id, relation },
      {
        onSuccess: () => {
          setPage(null);
          onLinked(title);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="space-y-3 border-t border-border pt-3" aria-label={l.add} data-add-stator-link>
      <div className="grid gap-3 sm:grid-cols-2">
        <Select label={l.relation} value={relation} onChange={(event) => setRelation(event.target.value as StatorRelation)} data-stator-relation-select>
          {STATOR_RELATIONS.map((each) => (
            <option key={each} value={each}>
              {l.relations[each]}
            </option>
          ))}
        </Select>
        {page ? (
          <div className="space-y-1">
            <span className="block text-sm font-medium text-ink-muted">{l.page}</span>
            <p className="flex items-center gap-2 text-sm text-ink" data-stator-picked={page.id}>
              <span className="min-w-0 truncate">
                <span className="font-mono text-xs text-ink-muted">{page.spaceKey}</span> {page.title}
              </span>
              <Button variant="link" onClick={() => setPage(null)} data-action="pick-another-page">
                {l.change}
              </Button>
            </p>
          </div>
        ) : (
          <PagePicker label={l.page} onPick={setPage} />
        )}
      </div>
      {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
      <Button type="submit" variant="secondary" icon={<Icon.Link />} loading={create.isPending} disabled={!page} data-action="create-stator-link">
        {l.create}
      </Button>
    </form>
  );
}
