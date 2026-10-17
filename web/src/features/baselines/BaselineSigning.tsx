import { useState } from "react";
import { ApiError } from "@/api/client";
import { useBaselineSignatures, useSignBaseline, type Baseline } from "@/api/baselines";
import { Button, Card, SectionTitle, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SignatureDialog } from "@/features/reviews/SignatureDialog";
import { RELEASE, resumeBaselineSigning } from "@/features/reviews/signing";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const signedOn = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** Where a baseline's signing stands, as a list or a page says it in a word; empty while nobody signs it. */
export function signingWords(baseline: Baseline): string {
  const s = baseline.signing;
  if (!s) return "";
  if (s.signed) return t.control.signed(s.signatures, s.required);
  if (s.signatures > 0) return t.control.signing(s.signatures, s.required);
  return t.control.unsigned(s.required);
}

/** A baseline's signing (#205): who signs it, the signatures given, and the caller's own while they may give it. */
export function BaselineSigning({ baseline }: { baseline: Baseline }) {
  const signing = baseline.signing;
  // A sign-in at the provider comes back to this page with the proof, once.
  const [resumed] = useState(() => resumeBaselineSigning(baseline.id));
  const [open, setOpen] = useState(Boolean(resumed));
  const [notice, setNotice] = useState("");
  const { data: signatures } = useBaselineSignatures(baseline.id, Boolean(signing && signing.signatures > 0));
  if (!signing) {
    return (
      <p className="text-sm text-ink-muted" data-baseline-signing="none">
        {t.control.signingNone}
      </p>
    );
  }
  return (
    <Card className="space-y-3 p-4" data-baseline-signing={signing.signed ? "signed" : signing.signatures > 0 ? "signing" : "unsigned"}>
      <div className="flex flex-wrap items-center gap-2">
        <SectionTitle>{t.control.signingTitle}</SectionTitle>
        {signing.signed && (
          <Tag data-baseline-signed="">
            <Icon.Lock className="mr-1 size-3" aria-hidden="true" />
            {t.control.signedTag}
          </Tag>
        )}
      </div>
      <p className="text-sm text-ink">{signingWords(baseline)}</p>
      <p className="text-sm text-ink-muted">{t.control.signersAre(signing.signers.map((each) => each.name).join(", "))}</p>
      {signing.frozen && <p className="text-xs text-ink-subtle">{t.control.frozenHint}</p>}
      {signatures && signatures.length > 0 && (
        <ol className="space-y-1 text-sm" aria-label={t.control.signingTitle} data-baseline-signatures="">
          {signatures.map((each) => (
            <li key={each.signedAt + each.signerName} className="text-ink">
              {t.control.signedBy(each.signerName, signedOn.format(new Date(each.signedAt)), t.review.signedWith[each.method] ?? each.method)}
              <span className="block text-xs text-ink-muted">{each.meaning}</span>
            </li>
          ))}
        </ol>
      )}
      <p role="status" className="text-sm text-ink-muted empty:hidden">
        {notice}
      </p>
      {signing.canSign && (
        <Button icon={<Icon.Check />} onClick={() => setOpen(true)} data-action="sign-baseline">
          {t.control.sign}
        </Button>
      )}
      {open && (
        <SignDialog
          baseline={baseline}
          proof={resumed?.proof}
          failure={resumed?.failure}
          onClose={() => setOpen(false)}
          onSigned={() => {
            setOpen(false);
            setNotice(t.control.signedNotice);
          }}
        />
      )}
    </Card>
  );
}

function SignDialog({
  baseline,
  proof,
  failure,
  onClose,
  onSigned,
}: {
  baseline: Baseline;
  proof?: string;
  failure?: string;
  onClose: () => void;
  onSigned: () => void;
}) {
  const sign = useSignBaseline(baseline.id);
  const meaning = baseline.signing?.meaning ?? "";
  const fields = sign.error instanceof ApiError ? sign.error.fields : {};
  const back = new URL(window.location.href);
  back.searchParams.set("sign", RELEASE);
  back.searchParams.set("baseline", baseline.id);
  return (
    <SignatureDialog
      kind={RELEASE}
      title={t.control.signTitle}
      intro={t.control.signIntro}
      meaning={meaning}
      action={t.control.signAction}
      pending={sign.isPending}
      refusal={fields.password ?? sign.error?.message}
      proof={proof}
      failure={failure}
      back={back}
      onLeave={() => undefined}
      onSign={(body) => sign.mutate({ meaning, ...body }, { onSuccess: onSigned })}
      onClose={onClose}
    />
  );
}
