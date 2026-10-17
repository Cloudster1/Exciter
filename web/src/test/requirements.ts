import type { Requirement } from "@/api/requirements";

/** A requirement as the API answers it, editable by the reader unless a test says otherwise. */
export function aRequirement(overrides: Partial<Requirement> = {}): Requirement {
  return {
    id: "0195f000-0000-7000-8000-0000000000a1",
    key: "CHK-R1",
    projectKey: "CHK",
    number: 1,
    kind: "requirement",
    title: "Pay the basket",
    statement: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "The shopper pays at once." }] }] },
    rationale: null,
    type: null,
    attributes: {},
    revision: 1,
    status: null,
    owner: null,
    priority: null,
    labels: [],
    comments: { threads: 0, open: 0 },
    watching: { requirement: false, project: false },
    approval: { pendingReviewId: null, approvedRevision: null, current: false },
    parentId: null,
    outline: "",
    formerKeys: [],
    copiedFrom: null,
    reuse: null,
    createdAt: "2026-10-01T08:00:00Z",
    createdByName: "Ada Lovelace",
    updatedAt: "2026-10-01T08:00:00Z",
    updatedByName: "Ada Lovelace",
    trashedAt: null,
    lock: null,
    can: { edit: true, transition: true, delete: true, link: true, comment: true },
    ...overrides,
  };
}
