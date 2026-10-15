import { type QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { SUSPECTS_PAGE_SIZE } from "@/config";
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
/** An end that moved on since the link was confirmed against it (#36). */
export type MovedEnd = Wire["SuspectLinkEnd"];
/** A suspect link as a project's list shows it, both ends at once. */
export type SuspectLink = Wire["SuspectLink"];
export type Clearance = Wire["LinkClearance"];
export type ClearInput = Wire["LinkClearInput"];
export type Counts = LinkType["counts"];

export const SUSPECT_ENDS: readonly SuspectEnd[] = ["target", "source", "both", "none"];
export const COUNTS: readonly Counts[] = ["decomposition", "verification", "nothing"];

const linkTypesKey = ["link-types"] as const;
const projectLinkTypesKey = (projectKey: string) => [...linkTypesKey, projectKey.toUpperCase()] as const;
export const linksQueryKey = ["links"] as const;
const suspectsKey = ["suspects"] as const;
const linksKey = (reqKey: string) => [...linksQueryKey, reqKey.toUpperCase()] as const;

/** The installation's link types, retired ones included. */
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

/** Makes, changes and deletes link types, the installation's or, with a project, the project's own. */
export function useLinkTypeActions(projectKey?: string) {
  const queryClient = useQueryClient();
  // A type of the installation shows in every project's list, so every list asks again.
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

/** A window of the suspect links touching a project's requirements, the longest waiting first. */
export function useSuspects(projectKey: string, offset: number) {
  return useQuery({
    queryKey: [...suspectsKey, projectKey.toUpperCase(), offset],
    queryFn: async () =>
      (await api.GET("/projects/{projectKey}/suspects", { params: { path: { projectKey }, query: { limit: SUSPECTS_PAGE_SIZE, offset } } })).data!,
  });
}

/** Every confirmation of a link, newest first. */
export function useClearances(linkId: string) {
  return useQuery({
    queryKey: ["clearances", linkId],
    queryFn: async (): Promise<Clearance[]> => (await api.GET("/links/{linkID}/clearances", { params: { path: { linkID: linkId } } })).data!.clearances,
  });
}

/** Everything that shows whether a link is suspect asks again once one is cleared. */
export function refreshSuspects(queryClient: QueryClient) {
  return Promise.all(
    [linksQueryKey, suspectsKey, ["clearances"], ["requirements"], ["spec"], ["armature", "links"], ["stator", "links"]].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

/** Confirms a link against its ends as read, with a comment. */
export function useClearLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, ...body }: ClearInput & { id: string }): Promise<TraceLink> =>
      (await api.POST("/links/{linkID}/clear", { params: { path: { linkID: id } }, body })).data!.link,
    onSuccess: () => refreshSuspects(queryClient),
  });
}
