import type { Decision } from "@/api/reviews";

// A signature begun before a sign-in at the identity provider: the comment typed waits in this tab until the
// browser comes back, and the address says which review and decision it was for.

const KEPT = "exciter.signing";

/** Keeps a comment for the signature a sign-in at the provider interrupts. */
export function keepSigning(reviewId: string, decision: Decision, comment: string): void {
  try {
    sessionStorage.setItem(KEPT, JSON.stringify({ reviewId, decision, comment }));
  } catch {
    // Without storage the comment is typed again, which is all that is lost.
  }
}

/** What the browser came back with: which review and decision, the proof or why there is none, and the comment. */
export interface Resumed {
  reviewId: string;
  decision: Decision;
  proof?: string;
  failure?: string;
  comment: string;
}

/** Reads, once, a signature the browser came back to finish, and takes it out of the address. */
export function resumeSigning(): Resumed | undefined {
  const url = new URL(window.location.href);
  const reviewId = url.searchParams.get("review");
  const decision = url.searchParams.get("sign");
  if (!reviewId || (decision !== "approve" && decision !== "reject")) return undefined;
  let comment = "";
  try {
    const kept = JSON.parse(sessionStorage.getItem(KEPT) ?? "null") as { reviewId?: string; comment?: string } | null;
    if (kept?.reviewId === reviewId) comment = kept.comment ?? "";
    sessionStorage.removeItem(KEPT);
  } catch {
    comment = "";
  }
  const resumed: Resumed = {
    reviewId,
    decision,
    proof: url.searchParams.get("signature") ?? undefined,
    failure: url.searchParams.get("signature_error") ?? undefined,
    comment,
  };
  for (const name of ["review", "sign", "signature", "signature_error"]) url.searchParams.delete(name);
  window.history.replaceState(window.history.state, "", url.pathname + url.search + url.hash);
  return resumed;
}
