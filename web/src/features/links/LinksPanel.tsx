import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { groupLinks, linkWays, useLinkActions, useLinks, useProjectLinkTypes, type Direction, type TraceLink } from "@/api/links";
import type { Requirement } from "@/api/requirements";
import type { QuickHit } from "@/api/search";
import type { StatusCategory } from "@/api/workflows";
import { Button, Card, ErrorBanner, IconButton, SectionTitle, Select, Skeleton, Tag, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { StatusBadge } from "@/features/workflows/StatusBadge";
import { t } from "@/i18n";
import { RequirementPicker } from "./RequirementPicker";
import { ClearLinkDialog, SuspectBadge, type CheckedLink, type LinkEnd } from "./Suspect";

const capitalized = (phrase: string) => phrase.charAt(0).toUpperCase() + phrase.slice(1);

/** A link as the requirement it was read from holds it, both ends put in their places. */
export function checked(link: TraceLink, here: LinkEnd): CheckedLink {
  const other = { key: link.other.key, revision: link.other.revision };
  const outgoing = link.direction === "outgoing";
  return { id: link.id, forward: link.type.forward, source: outgoing ? here : other, target: outgoing ? other : here, ends: link.suspect };
}

/** A requirement's trace links grouped by type and direction, each a way to the other end; its editors link
 * it to more and remove links. */
export function LinksPanel({ requirement }: { requirement: Requirement }) {
  const section = requirement.kind === "section";
  const { data: links, error, refetch } = useLinks(requirement.key, !section);
  const actions = useLinkActions(requirement.key);
  const toast = useToast();
  const [checking, setChecking] = useState<TraceLink | null>(null);
  const mayLink = requirement.can.link;

  function remove(link: TraceLink) {
    actions.create.reset();
    actions.remove.mutate(link, { onSuccess: () => toast.success(t.links.removed(link.phrase, link.other.key)) });
  }

  return (
    <Card className="space-y-3 p-4" data-requirement-links>
      <section aria-labelledby="links-title" className="space-y-3">
        <SectionTitle id="links-title">{t.links.title}</SectionTitle>
        {section && <p className="text-sm text-ink-subtle">{t.links.sectionNone}</p>}
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        {!section && !links && !error && <Skeleton />}
        {links && links.length === 0 && <p className="text-sm text-ink-subtle">{t.links.none}</p>}
        {links &&
          groupLinks(links).map((group) => (
            <div key={`${group.type.id}:${group.direction}`} className="space-y-1" data-link-group={`${group.type.key}:${group.direction}`}>
              <h3 className="text-sm font-medium text-ink">{capitalized(group.phrase)}</h3>
              <ul className="space-y-1">
                {group.links.map((link) => (
                  <li key={link.id} className="flex flex-wrap items-center gap-2 text-sm" data-link={link.other.key}>
                    <Link
                      to="/p/$projectKey/r/$reqKey"
                      params={{ projectKey: link.other.projectKey, reqKey: link.other.key }}
                      className="inline-flex items-baseline gap-2 hover:text-accent hover:underline"
                      title={t.links.linkedAt(link.sourceRevision, link.targetRevision)}
                    >
                      <span className="font-mono text-xs text-ink-muted">{link.other.key}</span>
                      <span className="font-medium text-ink">{link.other.title}</span>
                    </Link>
                    {link.other.status && <StatusBadge name={link.other.status.name} category={link.other.status.category as StatusCategory} />}
                    {link.other.trashed && <Tag>{t.links.trashed}</Tag>}
                    {link.suspect.length > 0 && <SuspectBadge />}
                    {link.suspect.length > 0 && mayLink && (
                      <Button variant="link" onClick={() => setChecking(link)} aria-label={t.suspect.checkLabel(link.other.key)} data-action="check-link">
                        {t.suspect.check}
                      </Button>
                    )}
                    {mayLink && (
                      <IconButton
                        icon={<Icon.X className="size-3" />}
                        label={t.links.remove(link.phrase, link.other.key)}
                        size="xs"
                        onClick={() => remove(link)}
                        data-action="remove-link"
                      />
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        {actions.remove.error && <ErrorBanner>{actions.remove.error.message}</ErrorBanner>}
        {mayLink && !section && (
          <AddLink
            requirement={requirement}
            onLinked={(link) => {
              actions.remove.reset();
              toast.success(t.links.added(link.phrase, link.other.key));
            }}
          />
        )}
        {checking && (
          <ClearLinkDialog
            link={checked(checking, { key: requirement.key, revision: requirement.revision })}
            onCleared={() => toast.success(t.suspect.cleared(checking.other.key))}
            onClose={() => setChecking(null)}
          />
        )}
      </section>
    </Card>
  );
}

/** Picks a type read from this requirement and the requirement at the other end, and links the two. */
function AddLink({ requirement, onLinked }: { requirement: Requirement; onLinked: (link: TraceLink) => void }) {
  const { data: types } = useProjectLinkTypes(requirement.projectKey);
  const { create } = useLinkActions(requirement.key);
  const [choice, setChoice] = useState("");
  const [other, setOther] = useState<QuickHit | null>(null);
  const ways = linkWays(types ?? [], t.links.reads);
  const picked = choice || ways[0]?.value || "";
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const refusal = create.error ? (fields.otherKey ?? fields.typeId ?? fields.direction ?? create.error.message) : "";

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!other || !picked) return;
    const [typeId, direction] = picked.split(":") as [string, Direction];
    create.mutate(
      { typeId, direction, otherKey: other.requirement.key },
      {
        onSuccess: (link) => {
          setOther(null);
          onLinked(link);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="space-y-3 border-t border-border pt-3" aria-label={t.links.add} data-add-link>
      <div className="grid gap-3 sm:grid-cols-2">
        <Select label={t.links.linkType} value={picked} onChange={(event) => setChoice(event.target.value)} data-link-type-select>
          {ways.map((way) => (
            <option key={way.value} value={way.value}>
              {way.label}
            </option>
          ))}
        </Select>
        {other ? (
          <div className="space-y-1">
            <span className="block text-sm font-medium text-ink-muted">{t.links.requirement}</span>
            <p className="flex items-center gap-2 text-sm text-ink" data-link-picked={other.requirement.key}>
              <span className="min-w-0 truncate">{t.links.picked(other.requirement.key, other.requirement.title)}</span>
              <Button variant="link" onClick={() => setOther(null)} data-action="pick-another">
                {t.links.change}
              </Button>
            </p>
          </div>
        ) : (
          <RequirementPicker label={t.links.requirement} exclude={[requirement.id]} onPick={setOther} />
        )}
      </div>
      {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
      <Button type="submit" variant="secondary" icon={<Icon.Link />} loading={create.isPending} disabled={!other || !picked} data-action="create-link">
        {t.links.create}
      </Button>
    </form>
  );
}
