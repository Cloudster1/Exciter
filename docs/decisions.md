# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-10-12: Requirement keys are `CHK-R42` (#1)

A requirement's key is its project key, `-R` and a number per project:
`^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$`. Armature's commit and mail scanners,
its query language and Stator's issue chips all look for a project key, a
hyphen and a digit, so none of them mistakes a requirement for an issue, and
the same project key can be used in Armature and Exciter without ambiguity.
Keys such as `SYS-12`, a type prefix and a number, are therefore not allowed.
A unit test feeds Exciter keys through copies of their patterns (#13).

## 2026-10-12: A revision is a normative save (#1)

A save writes an immutable revision when title, statement, rationale, type or
attributes change. Status, owner, labels and the place in the tree are logged
but never revisioned, so changing them neither voids an approval nor makes a
link suspect. Links are not part of a revision either: a revision of B for
every link to B would make everything below B suspect. Baselines capture the
link set instead. Editing an approved requirement is allowed; the new revision
supersedes the approval and the workflow takes the status back.

## 2026-10-12: The status category of a linked issue is stored for everyone (#1)

Stator shows Armature data only as the viewing person sees it. Coverage needs
to know whether an implementing issue is done for every reader, so Exciter
stores the status category of each linked issue, updated by Armature's
webhooks, and fetches summary and assignee live as the viewer.
**Reconsider if** Armature adds a way to answer status for an organization
without a person's token.

## 2026-10-12: Stack ports from 10000, and Armature removal wins (#1)

Each checkout's stack takes a slot of 20 ports from 10000 to 19999, derived
from its path. Stator uses 20000 to 29999 and the kernel hands out ephemeral
ports from 32768, so neither collides; slots holding 16686 (Armature's
Jaeger), 11211, 15672 or 10250 are skipped.

When someone removes Exciter's link from an issue in Armature, the trace link
is marked detached and not pushed again: the person's decision in Armature
wins over the sync.

## 2026-10-12: The tooling is Stator's (#1)

Exciter ports Stator's gate, hooks, release script and CI rather than
Armature's, because Stator's are the newer of the two and already split the
gate between a quick local run and CI. Stator is under Apache-2.0 since
`a5661a6`; `NOTICE` names the commit each ported file came from. A layer
joins the gate in the change that introduces it, so the gate is green from
the first commit.
