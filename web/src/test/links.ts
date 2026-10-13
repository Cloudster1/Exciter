import type { LinkType, TraceLink } from "@/api/links";

/** A link type as the API answers it: the organization's derives unless a test says otherwise. */
export function aLinkType(overrides: Partial<LinkType> = {}): LinkType {
  return {
    id: "0195f000-0000-7000-8000-0000000001a1",
    key: "derives",
    name: "Derives",
    forward: "derives from",
    backward: "is derived into",
    acyclic: true,
    suspectEnd: "target",
    counts: "decomposition",
    scope: "org",
    projectKey: "",
    enabled: true,
    rules: [],
    links: 0,
    retired: false,
    updatedAt: "2026-10-01T08:00:00Z",
    ...overrides,
  };
}

export const derives = aLinkType();
export const verifies = aLinkType({
  id: "0195f000-0000-7000-8000-0000000001a4",
  key: "verifies",
  name: "Verifies",
  forward: "verifies",
  backward: "is verified by",
  counts: "verification",
});
export const relates = aLinkType({
  id: "0195f000-0000-7000-8000-0000000001a6",
  key: "relates",
  name: "Relates",
  forward: "relates to",
  backward: "relates to",
  acyclic: false,
  suspectEnd: "none",
  counts: "nothing",
});

/** A link of CHK-R1 as it reads it, to the requirement other names. */
export function aLink(other: { key: string; title: string }, overrides: Partial<TraceLink> = {}): TraceLink {
  const type = overrides.type ?? derives;
  const direction = overrides.direction ?? "outgoing";
  return {
    id: `0195f000-0000-7000-8000-${other.key.replace(/\D/g, "").padStart(12, "0")}`,
    type,
    direction,
    phrase: direction === "outgoing" ? type.forward : type.backward,
    other: {
      id: `0195f000-0000-7000-8000-0000000002${other.key.replace(/\D/g, "").padStart(2, "0")}`,
      key: other.key,
      projectKey: other.key.split("-")[0]!,
      kind: "requirement",
      title: other.title,
      revision: 1,
      status: null,
      trashed: false,
    },
    sourceRevision: 1,
    targetRevision: 1,
    createdAt: "2026-10-01T08:00:00Z",
    createdByName: "Ada Lovelace",
    ...overrides,
  };
}
