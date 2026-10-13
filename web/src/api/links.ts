import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A kind of link, read from its source: the source derives from the target. */
export type LinkType = Wire["LinkType"];
export type LinkTypeInput = Wire["LinkTypeInput"];
export type LinkTypeRule = Wire["LinkTypeRule"];
export type ProjectLinkTypeInput = Wire["ProjectLinkTypeInput"];
/** One link as one of its ends reads it. */
export type TraceLink = Wire["TraceLink"];
export type LinkInput = Wire["TraceLinkInput"];
export type Direction = TraceLink["direction"];
export type SuspectEnd = LinkType["suspectEnd"];
export type Counts = LinkType["counts"];

export const SUSPECT_ENDS: readonly SuspectEnd[] = ["target", "source", "both", "none"];
export const COUNTS: readonly Counts[] = ["decomposition", "verification", "nothing"];

const linkTypesKey = ["link-types"] as const;
const projectLinkTypesKey = (projectKey: string) => [...linkTypesKey, projectKey.toUpperCase()] as const;
export const linksQueryKey = ["links"] as const;
const linksKey = (reqKey: string) => [...linksQueryKey, reqKey.toUpperCase()] as const;

/** The organization's link types, retired ones included. */
export function useOrgLinkTypes() {
  return useQuery({
    queryKey: [...linkTypesKey, ""],
    queryFn: async (): Promise<LinkType[]> => (await api.GET("/link-types")).data!.linkTypes,
  });
}

/** The link types a project may use, with whether it does and the pairs each joins there. */
export function useProjectLinkTypes(projectKey: string) {
  return useQuery({
    queryKey: projectLinkTypesKey(projectKey),
    queryFn: async (): Promise<LinkType[]> => (await api.GET("/projects/{projectKey}/link-types", { params: { path: { projectKey } } })).data!.linkTypes,
    enabled: Boolean(projectKey),
  });
}

/** Makes, changes and deletes link types, the organization's or, with a project, the project's own. */
export function useLinkTypeActions(projectKey?: string) {
  const queryClient = useQueryClient();
  // A type of the organization shows in every project's list, so every list asks again.
  const refresh = () => Promise.all([queryClient.invalidateQueries({ queryKey: linkTypesKey }), queryClient.invalidateQueries({ queryKey: linksQueryKey })]);
  return {
    create: useMutation({
      mutationFn: async (body: LinkTypeInput): Promise<LinkType> =>
        projectKey
          ? (await api.POST("/projects/{projectKey}/link-types", { params: { path: { projectKey } }, body })).data!.linkType
          : (await api.POST("/link-types", { body })).data!.linkType,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: LinkTypeInput & { id: string }): Promise<LinkType> =>
        (await api.PATCH("/link-types/{linkTypeID}", { params: { path: { linkTypeID: id } }, body })).data!.linkType,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/link-types/{linkTypeID}", { params: { path: { linkTypeID: id } } });
      },
      onSuccess: refresh,
    }),
    use: useMutation({
      mutationFn: async ({ id, ...body }: ProjectLinkTypeInput & { id: string }): Promise<LinkType> =>
        (await api.PUT("/projects/{projectKey}/link-types/{linkTypeID}", { params: { path: { projectKey: projectKey ?? "", linkTypeID: id } }, body })).data!
          .linkType,
      onSuccess: refresh,
    }),
  };
}

/** A requirement's live links in both directions, to requirements the reader may view. */
export function useLinks(reqKey: string, enabled = true) {
  return useQuery({
    queryKey: linksKey(reqKey),
    queryFn: async (): Promise<TraceLink[]> => (await api.GET("/requirements/{reqKey}/links", { params: { path: { reqKey } } })).data!.links,
    enabled,
  });
}

/** Links a requirement and removes its links; both ends' lists and histories ask again. */
export function useLinkActions(reqKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => Promise.all([queryClient.invalidateQueries({ queryKey: linksQueryKey }), queryClient.invalidateQueries({ queryKey: ["history"] })]);
  return {
    create: useMutation({
      mutationFn: async (body: LinkInput): Promise<TraceLink> =>
        (await api.POST("/requirements/{reqKey}/links", { params: { path: { reqKey } }, body })).data!.link,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (link: TraceLink): Promise<TraceLink> => {
        await api.DELETE("/links/{linkID}", { params: { path: { linkID: link.id } } });
        return link;
      },
      onSuccess: refresh,
    }),
  };
}

/** Whether a type reads the same from both ends, so it is offered once. */
export function symmetric(type: LinkType): boolean {
  return type.forward === type.backward;
}

/** Links gathered under their type and direction, in the order the server lists them. */
export function groupLinks(links: TraceLink[]): { type: LinkType; direction: Direction; phrase: string; links: TraceLink[] }[] {
  const groups: { type: LinkType; direction: Direction; phrase: string; links: TraceLink[] }[] = [];
  for (const link of links) {
    const direction = symmetric(link.type) ? "outgoing" : link.direction;
    const group = groups.find((each) => each.type.id === link.type.id && each.direction === direction);
    if (group) group.links.push(link);
    else groups.push({ type: link.type, direction, phrase: link.phrase, links: [link] });
  }
  return groups;
}
