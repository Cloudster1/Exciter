import { keepPreviousData, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { GLOSSARY_PAGE_SIZE, TERM_USES_PAGE_SIZE } from "@/config";
import { api } from "./client";
import { requirementQueryKey } from "./requirements";
import type { components } from "./schema";

// The glossary (#210): a project's terms and their revisions, the requirements that use each, the terms a
// requirement uses, and the library projects whose glossaries a project takes.

type Wire = components["schemas"];

export type Term = Wire["Term"];
export type TermRef = Wire["TermRef"];
export type TermInput = Wire["TermInput"];
export type TermPatch = Wire["TermPatch"];
export type TermRevision = Wire["TermRevision"];
export type TermUse = Wire["TermUse"];
export type RequirementTerm = Wire["RequirementTerm"];
export type Glossaries = Wire["Glossaries"];
export type GlossaryLibrary = Wire["GlossaryLibrary"];
export type TermHit = Wire["TermHit"];
export type TermStatus = Term["status"];

/** Every status, in the order a choice lists them. */
export const TERM_STATUSES: readonly TermStatus[] = ["proposed", "approved", "deprecated"];

/** What the glossary page narrows its list by. */
export interface TermFilter {
  q?: string;
  status?: TermStatus | "";
  /** The project's own terms alone, without its libraries'. */
  own?: boolean;
  offset?: number;
  limit?: number;
}

const termsKey = ["terms"] as const;
const termKey = (termId: string) => ["term", termId] as const;
const requirementTermsKey = (reqKey: string) => ["requirement-terms", reqKey.toUpperCase()] as const;
const glossariesKey = (projectKey: string) => ["glossaries", projectKey.toUpperCase()] as const;

/** A window of the terms a project reads by: its own first, then each library's. */
export function useTerms(projectKey: string, filter: TermFilter = {}, enabled = true) {
  const query = {
    ...(filter.q ? { q: filter.q } : {}),
    ...(filter.status ? { status: [filter.status] } : {}),
    ...(filter.own ? { own: true } : {}),
    limit: filter.limit ?? GLOSSARY_PAGE_SIZE,
    offset: filter.offset ?? 0,
  };
  return useQuery({
    queryKey: [...termsKey, projectKey.toUpperCase(), query],
    queryFn: async () => (await api.GET("/projects/{projectKey}/terms", { params: { path: { projectKey }, query } })).data!,
    placeholderData: keepPreviousData,
    enabled,
  });
}

/** One term, with its definition. */
export function useTerm(termId: string, enabled = true) {
  return useQuery({
    queryKey: termKey(termId),
    queryFn: async (): Promise<Term> => (await api.GET("/terms/{termID}", { params: { path: { termID: termId } } })).data!.term,
    enabled: enabled && Boolean(termId),
  });
}

/** Every revision of a term, the newest first as the API lists them. */
export function useTermRevisions(termId: string) {
  return useQuery({
    queryKey: [...termKey(termId), "revisions"],
    queryFn: async (): Promise<TermRevision[]> => (await api.GET("/terms/{termID}/revisions", { params: { path: { termID: termId } } })).data!.revisions,
  });
}

/** The requirements the reader may read that use a term, a window at a time. */
export function useTermUses(termId: string, offset: number) {
  return useQuery({
    queryKey: [...termKey(termId), "uses", offset],
    queryFn: async () => (await api.GET("/terms/{termID}/uses", { params: { path: { termID: termId }, query: { limit: TERM_USES_PAGE_SIZE, offset } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** The terms a requirement uses, by its words or by an explicit link, each saying whether it is suspect. */
export function useRequirementTerms(reqKey: string, enabled = true) {
  return useQuery({
    queryKey: requirementTermsKey(reqKey),
    queryFn: async (): Promise<RequirementTerm[]> => (await api.GET("/requirements/{reqKey}/terms", { params: { path: { reqKey } } })).data!.terms,
    enabled,
  });
}

// A term's words decide which requirements use it and what their findings say, and its definition what is suspect,
// so a change reaches every view that counts either.
function afterTermChange(queryClient: QueryClient) {
  return Promise.all(
    [termsKey, ["term"], ["requirement-terms"], ["quality-check"], ["quality"], ["requirements"], ["tree"], ["spec"], ["search"], ["suspects"]].map(
      (queryKey) => queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

/** Adds a term to a project's glossary. */
export function useCreateTerm(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: TermInput): Promise<Term> =>
      (await api.POST("/projects/{projectKey}/terms", { params: { path: { projectKey } }, body })).data!.term,
    onSuccess: async (term) => {
      queryClient.setQueryData(termKey(term.id), term);
      await afterTermChange(queryClient);
    },
  });
}

/** Changes a term over the revision it was read at; a newer one refuses with stale_revision. */
export function useUpdateTerm(termId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: TermPatch): Promise<Term> => (await api.PATCH("/terms/{termID}", { params: { path: { termID: termId } }, body })).data!.term,
    onSuccess: async (term) => {
      queryClient.setQueryData(termKey(term.id), term);
      await afterTermChange(queryClient);
    },
  });
}

/** Deletes a term and its revisions; words linked to it read as plain words from then on. */
export function useDeleteTerm(termId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/terms/{termID}", { params: { path: { termID: termId } } });
    },
    onSuccess: async () => {
      queryClient.removeQueries({ queryKey: termKey(termId) });
      await afterTermChange(queryClient);
    },
  });
}

/** Confirms a requirement still holds under a term's current definition, so its use is no longer suspect. */
export function useClearTermUse(reqKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ termId, termRevision, comment }: { termId: string; termRevision: number; comment: string }) =>
      (
        await api.POST("/requirements/{reqKey}/terms/{termID}/clear", {
          params: { path: { reqKey, termID: termId } },
          body: { termRevision, comment },
        })
      ).data!.term,
    onSuccess: async (_term, { termId }) => {
      await Promise.all(
        [
          requirementTermsKey(reqKey),
          [...termKey(termId), "uses"],
          requirementQueryKey(reqKey),
          ["tree"],
          ["spec"],
          ["requirements", "list"],
          ["suspects"],
        ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
      );
    },
  });
}

/** The library projects whose glossaries a project takes, in the order they win a word. */
export function useGlossaries(projectKey: string) {
  return useQuery({
    queryKey: glossariesKey(projectKey),
    queryFn: async (): Promise<Glossaries> => (await api.GET("/projects/{projectKey}/glossaries", { params: { path: { projectKey } } })).data!.glossaries,
  });
}

/** Names the library projects, in order; the project's requirements are read by their terms from then on. */
export function useSaveGlossaries(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (libraries: string[]): Promise<Glossaries> =>
      (await api.PUT("/projects/{projectKey}/glossaries", { params: { path: { projectKey } }, body: { libraries } })).data!.glossaries,
    onSuccess: async (saved) => {
      queryClient.setQueryData(glossariesKey(projectKey), saved);
      await afterTermChange(queryClient);
    },
  });
}
