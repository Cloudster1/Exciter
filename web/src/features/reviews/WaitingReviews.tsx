import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useWaitingReviews } from "@/api/reviews";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { REVIEWS_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { MySessions } from "@/features/sessions/SessionList";
import { localDateFormat } from "@/lib/format";

// The reviews waiting on the reader (#35), after Stator's WaitingReviews.

const askedOn = localDateFormat({ dateStyle: "medium" });

/** Every review waiting for the reader's decision, the oldest first. */
export function WaitingReviewsScreen() {
  const [offset, setOffset] = useState(0);
  const { data, error, refetch, isLoading } = useWaitingReviews(offset);
  const rows = data?.reviews ?? [];
  const total = data?.total ?? 0;
  return (
    <Page width="narrow" data-waiting-reviews="">
      <PageHeader title={t.review.inboxTitle} meta={t.review.inboxIntro} />
      <MySessions />
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton />
      ) : rows.length === 0 ? (
        <EmptyState icon={<Icon.Check />} title={t.review.inboxEmpty} />
      ) : (
        <>
          <ul className="flex flex-col divide-y divide-border" aria-label={t.review.inboxTitle}>
            {rows.map((review) => (
              <li key={review.id} className="flex min-w-0 flex-col gap-0.5 py-3" data-waiting-review={review.id}>
                <Link
                  to="/projects/$projectKey/r/$reqKey"
                  params={{ projectKey: review.requirement.projectKey, reqKey: review.requirement.key }}
                  className="truncate text-sm font-medium text-ink hover:text-accent hover:underline"
                >
                  <span className="font-mono">{review.requirement.key}</span> {review.requirement.title}
                </Link>
                <span className="flex flex-wrap items-center gap-x-1.5 text-xs text-ink-subtle">
                  <span>{t.review.inboxRow(review.requestedByName, askedOn.format(new Date(review.requestedAt)))}</span>
                  <span aria-hidden="true">·</span>
                  <span data-waiting-approvals={`${review.approvals}/${review.required}`}>{t.review.approvals(review.approvals, review.required)}</span>
                  <span aria-hidden="true">·</span>
                  <Link
                    to="/projects/$projectKey/spec"
                    params={{ projectKey: review.requirement.projectKey }}
                    search={{ waiting: true, changes: "approved" }}
                    className="hover:text-accent hover:underline"
                    data-review-in-document={review.requirement.projectKey}
                  >
                    {t.review.inDocument(review.requirement.projectKey)}
                  </Link>
                </span>
              </li>
            ))}
          </ul>
          {total > REVIEWS_PAGE_SIZE && (
            <nav aria-label={t.review.inboxTitle} className="mt-4 flex items-center justify-between gap-2">
              <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - REVIEWS_PAGE_SIZE))}>
                {t.review.previous}
              </Button>
              <span className="text-sm text-ink-subtle tabular-nums">{t.review.range(offset + 1, offset + rows.length, total)}</span>
              <Button variant="secondary" size="sm" disabled={offset + REVIEWS_PAGE_SIZE >= total} onClick={() => setOffset(offset + REVIEWS_PAGE_SIZE)}>
                {t.review.next}
              </Button>
            </nav>
          )}
        </>
      )}
    </Page>
  );
}
