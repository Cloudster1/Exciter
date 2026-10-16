import { Link } from "@tanstack/react-router";
import type { Requirement } from "@/api/requirements";
import { useMakeVariant, useTakeSource } from "@/api/reuse";
import { Button, ErrorBanner } from "@/components/ui";
import { t } from "@/i18n";

/**
 * What a reused requirement follows (#49): its library source and the revision it shows, whether the source has
 * moved on, and the take and the variant an editor may choose.
 */
export function ReuseBanner({ requirement, onNotice }: { requirement: Requirement; onNotice: (message: string) => void }) {
  const take = useTakeSource(requirement.key);
  const variant = useMakeVariant(requirement.key);
  const reuse = requirement.reuse;
  if (!reuse) return null;
  const source = reuse.source;
  const following = reuse.state === "following";
  const mayChange = requirement.can.edit;
  const error = take.error ?? variant.error;
  // A variant takes the source to follow again, behind or not; a copy that follows takes it only once it moved on.
  const mayTake = mayChange && !!source && (reuse.behind || !following);
  const mayBranch = mayChange && following;

  let said: string;
  if (source) said = following ? t.reuse.following(source.key, reuse.revision) : t.reuse.variant(source.key, reuse.revision);
  else said = following ? t.reuse.followingHidden(reuse.revision) : t.reuse.variantHidden(reuse.revision);

  return (
    <div
      className="space-y-2 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink"
      data-reuse={reuse.state}
      data-reuse-behind={reuse.behind}
    >
      <p>
        {source ? (
          <Link to="/p/$projectKey/r/$reqKey" params={{ projectKey: source.projectKey, reqKey: source.key }} className="hover:text-accent hover:underline">
            {said}
          </Link>
        ) : (
          said
        )}
      </p>
      {source && reuse.behind && (
        <p className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span data-reuse-source-revision={source.revision}>{t.reuse.behind(source.key, source.revision)}</span>
          <Link
            to="/p/$projectKey/r/$reqKey/history"
            params={{ projectKey: source.projectKey, reqKey: source.key }}
            search={{ from: reuse.revision, to: source.revision }}
            className="text-accent hover:underline"
            data-reuse-diff=""
          >
            {t.reuse.whatChanged}
          </Link>
        </p>
      )}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {(mayTake || mayBranch) && (
        <div className="flex flex-wrap gap-2">
          {mayTake && source && (
            <Button
              size="sm"
              loading={take.isPending}
              onClick={() => take.mutate(source.revision, { onSuccess: () => onNotice(t.reuse.taken(source.revision)) })}
              data-action="take-source"
            >
              {following ? t.reuse.take(source.revision) : t.reuse.takeAgain(source.revision)}
            </Button>
          )}
          {mayBranch && (
            <Button
              size="sm"
              variant="secondary"
              loading={variant.isPending}
              onClick={() => variant.mutate(undefined, { onSuccess: () => onNotice(t.reuse.madeVariant) })}
              data-action="make-variant"
            >
              {t.reuse.makeVariant}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
