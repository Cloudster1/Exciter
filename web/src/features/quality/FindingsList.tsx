import { useId, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useAcceptFinding, useWithdrawAcceptance, type QualityFinding, type QualitySentence, type Severity } from "@/api/quality";
import { Button, Dialog, ErrorBanner, Field, Tag, cx } from "@/components/ui";
import { QUALITY_REASON_MAX_LENGTH, QUALITY_REASON_ROWS } from "@/config";
import type { FindingMark } from "@/features/editor/quality";
import { t } from "@/i18n";

/** The findings a reader is shown: those that count and those accepted, never those of a rule switched off. */
export function shownFindings(findings: QualityFinding[]): QualityFinding[] {
  return findings.filter((f) => f.active || f.accepted);
}

/** The statement's findings as the editor and the reader mark them. */
export function findingMarks(findings: QualityFinding[]): FindingMark[] {
  return shownFindings(findings)
    .filter((f) => f.field === "statement")
    .map((f) => ({
      key: f.key,
      severity: f.severity,
      block: f.block,
      from: f.from,
      to: f.to,
      accepted: Boolean(f.accepted),
      title: `${t.quality.rules[f.rule].name}: ${t.quality.rules[f.rule].fix}`,
    }));
}

const severityTone: Record<Severity, string> = {
  error: "border-danger bg-danger-subtle text-ink",
  warning: "border-warning bg-warning-subtle text-ink",
  hint: "",
};

/** Where accepting is offered: the requirement's current revision, and the keys it has when only those may be. */
export interface Acceptance {
  reqKey: string;
  revision: number;
  /** The keys of the saved revision; a finding new in a draft is accepted once the draft is saved. */
  keys?: ReadonlySet<string>;
  /** False shows acceptances without offering to give or take one back. */
  may: boolean;
}

/** The findings of the writing rules as a list (#207), the twin of the marks in the text for a keyboard and a screen
 * reader: each rule, its words and how to fix it, with the way to its words and, on a saved requirement, to accept it. */
export function FindingsList({
  findings,
  sentences = [],
  score,
  checking = false,
  failed = false,
  onShow,
  accept,
}: {
  findings: QualityFinding[];
  sentences?: QualitySentence[];
  score?: number | null;
  checking?: boolean;
  failed?: boolean;
  onShow?: (finding: QualityFinding) => void;
  accept?: Acceptance;
}) {
  const l = t.quality;
  const shown = shownFindings(findings);
  const statementSentences = sentences.filter((s) => s.field === "statement");
  const [accepting, setAccepting] = useState<QualityFinding | null>(null);
  const titleId = useId();
  return (
    <section aria-labelledby={titleId} className="space-y-2 rounded-control border border-border p-3" data-findings>
      <div className="flex flex-wrap items-baseline gap-2">
        {/* A heading would sit inside the entry being edited, among that entry's own headings. */}
        <p id={titleId} className="text-2xs font-medium tracking-wide text-ink-subtle uppercase">
          {l.findingsTitle}
        </p>
        {typeof score === "number" && <span className="text-sm text-ink-muted tabular-nums">{l.draftScore(score)}</span>}
        <span role="status" className="text-sm text-ink-muted" data-findings-count={shown.length}>
          {failed ? l.checkFailed : l.count(shown.length)}
        </span>
        {checking && <span className="text-xs text-ink-subtle">{l.checking}</span>}
      </div>
      {shown.length === 0 && !failed && <p className="text-sm text-ink-subtle">{l.none}</p>}
      {shown.length > 0 && (
        <ul className="flex flex-col divide-y divide-border">
          {shown.map((finding) => (
            <Finding
              key={finding.key}
              finding={finding}
              onShow={onShow}
              accept={accept}
              mayAccept={Boolean(accept?.may) && (!accept?.keys || accept.keys.has(finding.key))}
              onAccept={() => setAccepting(finding)}
            />
          ))}
        </ul>
      )}
      {statementSentences.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 pt-1" data-sentences>
          <span className="text-xs text-ink-muted">{l.sentences}</span>
          {statementSentences.map((sentence, i) => (
            <Tag key={`${sentence.block}:${sentence.from}`} data-sentence-pattern={sentence.pattern}>
              {l.sentence(i + 1, l.patterns[sentence.pattern])}
            </Tag>
          ))}
        </div>
      )}
      {accepting && accept && <AcceptDialog finding={accepting} reqKey={accept.reqKey} revision={accept.revision} onClose={() => setAccepting(null)} />}
    </section>
  );
}

function Finding({
  finding,
  onShow,
  accept,
  mayAccept,
  onAccept,
}: {
  finding: QualityFinding;
  onShow?: (finding: QualityFinding) => void;
  accept?: Acceptance;
  mayAccept: boolean;
  onAccept: () => void;
}) {
  const l = t.quality;
  const rule = l.rules[finding.rule];
  const withdraw = useWithdrawAcceptance(accept?.reqKey ?? "");
  return (
    <li className="space-y-1 py-2" data-finding-item={finding.key} data-rule={finding.rule} data-severity={finding.severity}>
      <div className="flex flex-wrap items-center gap-1.5">
        <Tag className={cx(severityTone[finding.severity])}>{l.severities[finding.severity]}</Tag>
        <span className="text-sm font-medium text-ink">{rule.name}</span>
        <span className="rounded bg-surface-raised px-1 font-mono text-xs text-ink" data-finding-match>
          {finding.match}
        </span>
        {finding.field === "title" && <Tag>{l.inTitle}</Tag>}
        {finding.accepted && <Tag data-accepted="">{l.acceptedTag}</Tag>}
      </div>
      <p className="text-sm text-ink-muted">{rule.fix}</p>
      {finding.accepted && (
        <p className="text-xs text-ink-muted" data-acceptance>
          {l.accepted(finding.accepted.by.name, finding.accepted.reason)}
          {finding.accepted.carriedFrom !== null && ` ${l.carried(finding.accepted.carriedFrom)}`}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {onShow && finding.field === "statement" && (
          <Button variant="ghost" size="sm" aria-label={l.showInTextLabel(finding.match)} onClick={() => onShow(finding)} data-action="show-finding">
            {l.showInText}
          </Button>
        )}
        {accept?.may && !finding.accepted && mayAccept && (
          <Button variant="secondary" size="sm" aria-label={l.acceptLabel(finding.match)} onClick={onAccept} data-action="accept-finding">
            {l.accept}
          </Button>
        )}
        {accept?.may && finding.accepted && (
          <Button
            variant="secondary"
            size="sm"
            aria-label={l.withdrawLabel(finding.match)}
            loading={withdraw.isPending}
            onClick={() => withdraw.mutate({ revision: accept.revision, key: finding.key })}
            data-action="withdraw-acceptance"
          >
            {l.withdraw}
          </Button>
        )}
      </div>
      {accept?.may && !finding.accepted && !mayAccept && <p className="text-xs text-ink-subtle">{l.savedOnly}</p>}
      {withdraw.error && <ErrorBanner>{withdraw.error.message}</ErrorBanner>}
    </li>
  );
}

function AcceptDialog({ finding, reqKey, revision, onClose }: { finding: QualityFinding; reqKey: string; revision: number; onClose: () => void }) {
  const l = t.quality;
  const [reason, setReason] = useState("");
  const accept = useAcceptFinding(reqKey);
  const fields = accept.error instanceof ApiError ? accept.error.fields : {};
  function submit(event: FormEvent) {
    event.preventDefault();
    accept.mutate({ revision, key: finding.key, reason: reason.trim() }, { onSuccess: onClose });
  }
  return (
    <Dialog open title={l.acceptTitle} onClose={onClose} attrs={{ "data-accept-finding": "" }}>
      <form onSubmit={submit} className="space-y-4" noValidate>
        <p className="text-sm text-ink">{l.acceptIntro(l.rules[finding.rule].name, finding.match)}</p>
        {accept.error && !fields.reason && <ErrorBanner>{accept.error.message}</ErrorBanner>}
        <Field
          label={l.reason}
          hint={l.reasonHint}
          value={reason}
          rows={QUALITY_REASON_ROWS}
          maxLength={QUALITY_REASON_MAX_LENGTH}
          error={fields.reason}
          onChange={(event) => setReason(event.target.value)}
          autoFocus
        />
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={accept.isPending} disabled={!reason.trim()} data-action="confirm-accept-finding">
            {l.acceptSend}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
