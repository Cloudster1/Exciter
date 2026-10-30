import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useAddSuiteDemo, useLinkSuiteDemo, useSuiteDemo, type SuiteDemo as Demo } from "@/api/installation";
import { Button, Card, Dialog, ErrorBanner, Skeleton } from "@/components/ui";
import { t } from "@/i18n";

/** The suite demo (#270): added once, built by the worker, and linked again once Armature or Stator can take more. */
export function SuiteDemo() {
  const { data: demo, isLoading, error } = useSuiteDemo();
  const add = useAddSuiteDemo();
  const relink = useLinkSuiteDemo();
  const [confirming, setConfirming] = useState(false);
  const failed = add.error ?? relink.error;
  const refusal = failed instanceof ApiError && failed.fields.key ? failed.fields.key : failed?.message;

  return (
    <Card className="space-y-4 p-4" data-suite-demo={demo?.state ?? "none"}>
      <div className="space-y-1">
        <h2 className="text-sm font-semibold text-ink">{t.suiteDemo.title}</h2>
        <p className="text-sm text-ink-muted">{t.suiteDemo.intro}</p>
      </div>
      {isLoading ? <Skeleton /> : error ? <ErrorBanner>{error.message}</ErrorBanner> : <Progress demo={demo ?? null} />}
      {refusal && !confirming && <ErrorBanner>{refusal}</ErrorBanner>}
      {!isLoading && !error && !demo && (
        <Button onClick={() => setConfirming(true)} data-action="add-suite-demo">
          {t.suiteDemo.add}
        </Button>
      )}
      {(demo?.state === "done" || demo?.state === "failed") && (
        <Button variant="secondary" onClick={() => relink.mutate()} loading={relink.isPending} data-action="link-suite-demo">
          {t.suiteDemo.relink}
        </Button>
      )}
      {confirming && (
        <Dialog
          open
          title={t.suiteDemo.confirmTitle}
          onClose={() => setConfirming(false)}
          attrs={{ "data-confirm-suite-demo": "" }}
          footer={
            <>
              <Button variant="secondary" onClick={() => setConfirming(false)}>
                {t.kit.cancel}
              </Button>
              <Button
                onClick={() => add.mutate(undefined, { onSuccess: () => setConfirming(false) })}
                loading={add.isPending}
                data-action="confirm-add-suite-demo"
              >
                {t.suiteDemo.confirm}
              </Button>
            </>
          }
        >
          <p className="text-sm text-ink">{t.suiteDemo.confirmBody}</p>
          {refusal && (
            <div className="mt-3">
              <ErrorBanner>{refusal}</ErrorBanner>
            </div>
          )}
        </Dialog>
      )}
    </Card>
  );
}

function Progress({ demo }: { demo: Demo | null }) {
  if (!demo) return null;
  const said =
    demo.state === "queued" || demo.state === "running"
      ? demo.kind === "links"
        ? t.suiteDemo.linking
        : t.suiteDemo.building(Math.min(demo.step + 1, demo.steps), demo.steps)
      : demo.state === "failed"
        ? t.suiteDemo.failed
        : t.suiteDemo.done(demo.requirements, demo.links, demo.skipped);
  return (
    <div className="space-y-2">
      <p role="status" className="text-sm text-ink" data-suite-demo-status>
        {said}
      </p>
      {demo.message && <p className="text-sm text-ink-muted">{demo.message}</p>}
      {demo.state === "done" && (
        <ul className="flex flex-wrap gap-3 text-sm" aria-label={t.suiteDemo.projects}>
          {demo.projectKeys.map((key) => (
            <li key={key}>
              <Link to="/projects/$projectKey" params={{ projectKey: key }} className="font-medium text-ink hover:text-accent hover:underline">
                {key}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
