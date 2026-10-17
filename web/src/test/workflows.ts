import type { Status, StatusCategory, Workflow } from "@/api/workflows";

/** A status as the API answers it, in no requirement unless a test says otherwise. */
export function aStatus(id: string, name: string, category: StatusCategory, overrides: Partial<Status> = {}): Status {
  return { id, name, category, description: "", position: 0, requirements: 0, locks: false, ...overrides };
}

export const draft = aStatus("0195f000-0000-7000-8000-000000000b01", "Draft", "draft", { position: 0, requirements: 2 });
export const review = aStatus("0195f000-0000-7000-8000-000000000b02", "In review", "review", { position: 1 });
export const approved = aStatus("0195f000-0000-7000-8000-000000000b03", "Approved", "approved", { position: 2, requirements: 1 });
export const rejected = aStatus("0195f000-0000-7000-8000-000000000b04", "Rejected", "closed", { position: 3 });

/** The heart of the default workflow: Draft to In review to Approved, a reject from anywhere asking why, and
 * Approved going back to Draft on a new revision. */
export function aWorkflow(overrides: Partial<Workflow> = {}): Workflow {
  return {
    id: "0195f000-0000-7000-8000-000000000c01",
    name: "Default",
    description: "",
    projectDefault: true,
    typeIds: [],
    updatedAt: "2026-10-01T08:00:00Z",
    steps: [
      { status: draft, isInitial: true, onRevisionStatusId: null, layout: { x: 40, y: 40 } },
      { status: review, isInitial: false, onRevisionStatusId: null, layout: null },
      { status: approved, isInitial: false, onRevisionStatusId: draft.id, layout: null },
      { status: rejected, isInitial: false, onRevisionStatusId: null, layout: null },
    ],
    transitions: [
      {
        id: "0195f000-0000-7000-8000-000000000d01",
        name: "Submit for review",
        description: "",
        fromStatusId: draft.id,
        toStatusId: review.id,
        position: 0,
        rules: [],
      },
      {
        id: "0195f000-0000-7000-8000-000000000d02",
        name: "Approve",
        description: "",
        fromStatusId: review.id,
        toStatusId: approved.id,
        position: 1,
        rules: [
          { id: "0195f000-0000-7000-8000-000000000e01", kind: "condition", type: "condition.project_role", config: { roles: ["reviewer"] }, position: 0 },
        ],
      },
      {
        id: "0195f000-0000-7000-8000-000000000d03",
        name: "Reject",
        description: "",
        fromStatusId: null,
        toStatusId: rejected.id,
        position: 2,
        rules: [{ id: "0195f000-0000-7000-8000-000000000e02", kind: "validator", type: "validator.comment_required", config: {}, position: 0 }],
      },
    ],
    ...overrides,
  };
}
