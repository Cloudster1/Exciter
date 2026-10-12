import type { Project } from "@/api/projects";

/** A project as the API answers it, administered by the reader unless a test says otherwise. */
export function aProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "0195f000-0000-7000-8000-0000000000f1",
    key: "CHK",
    name: "Checkout",
    description: "Paying for the basket.",
    settings: { defaultTypeId: null, workflowId: null, requireRevisionComment: false },
    template: "",
    formerKeys: [],
    requirements: 0,
    createdAt: "2026-10-01T08:00:00Z",
    updatedAt: "2026-10-01T08:00:00Z",
    archivedAt: null,
    archivedByName: "",
    can: { view: true, comment: true, edit: true, approve: true, administer: true, purge: true },
    ...overrides,
  };
}
