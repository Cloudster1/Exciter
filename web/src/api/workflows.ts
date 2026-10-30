import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { api } from "./client";
import { projectQueryKey } from "./projects";
import { requirementQueryKey, type Requirement } from "./requirements";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A step of a project's lifecycle, of one category that counts and reports read. */
export type Status = Wire["Status"];
export type StatusCategory = Status["category"];
export type StatusChanges = Wire["StatusInput"];
/** A set of statuses and the transitions between them, with the rules each transition carries. */
export type Workflow = Wire["Workflow"];
export type WorkflowStep = Wire["WorkflowStep"];
export type Transition = Wire["Transition"];
export type WorkflowInput = Wire["WorkflowInput"];
export type WorkflowAssignment = Wire["WorkflowAssignment"];
export type RuleType = Wire["RuleType"];
export type RuleKind = RuleType["kind"];
/** A transition the reader may take on a requirement now. */
export type AvailableTransition = Wire["AvailableTransition"];

/** The categories in the order a lifecycle runs through them. */
export const STATUS_CATEGORIES: StatusCategory[] = ["draft", "review", "approved", "done", "closed"];

const workflowsKey = ["workflows"] as const;

function statusesKey(projectKey: string) {
  return [...workflowsKey, "statuses", projectKey.toUpperCase()] as const;
}

function listKey(projectKey: string) {
  return [...workflowsKey, "list", projectKey.toUpperCase()] as const;
}

function transitionsKey(reqKey: string) {
  return ["transitions", reqKey.toUpperCase()] as const;
}

/** A project's statuses in order, each with how many requirements are in it. */
export function useStatuses(projectKey: string) {
  return useQuery({
    queryKey: statusesKey(projectKey),
    queryFn: async (): Promise<Status[]> => (await api.GET("/projects/{projectKey}/statuses", { params: { path: { projectKey } } })).data!.statuses,
    enabled: Boolean(projectKey),
  });
}

/** A project's workflows, each with the types that follow it. */
export function useWorkflows(projectKey: string) {
  return useQuery({
    queryKey: listKey(projectKey),
    queryFn: async (): Promise<Workflow[]> => (await api.GET("/projects/{projectKey}/workflows", { params: { path: { projectKey } } })).data!.workflows,
    enabled: Boolean(projectKey),
  });
}

/** One workflow, whole; an empty id reads nothing, for a workflow not made yet. */
export function useWorkflow(projectKey: string, id: string) {
  return useQuery({
    queryKey: [...listKey(projectKey), "one", id],
    queryFn: async (): Promise<Workflow> =>
      (await api.GET("/projects/{projectKey}/workflows/{workflowID}", { params: { path: { projectKey, workflowID: id } } })).data!.workflow,
    enabled: Boolean(projectKey && id),
  });
}

/** Every rule a transition may carry, which the server alone decides. */
export function useRuleTypes() {
  return useQuery({
    queryKey: ["workflow-rule-types"],
    queryFn: async (): Promise<RuleType[]> => (await api.GET("/workflow-rule-types")).data!.ruleTypes,
    staleTime: Infinity,
  });
}

/** Everything a status or a workflow changes: the lists here, the project's settings and the requirements' statuses. */
function useRefresh(projectKey: string) {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: workflowsKey }),
      queryClient.invalidateQueries({ queryKey: projectQueryKey(projectKey) }),
      queryClient.invalidateQueries({ queryKey: ["types", projectKey.toUpperCase()] }),
      queryClient.invalidateQueries({ queryKey: ["requirements"] }),
      queryClient.invalidateQueries({ queryKey: ["transitions"] }),
    ]);
}

/** Makes, changes and deletes a project's statuses. */
export function useStatusActions(projectKey: string) {
  const refresh = useRefresh(projectKey);
  const path = { projectKey };
  return {
    create: useMutation({
      mutationFn: async (body: StatusChanges): Promise<Status> => (await api.POST("/projects/{projectKey}/statuses", { params: { path }, body })).data!.status,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: StatusChanges & { id: string }): Promise<Status> =>
        (await api.PATCH("/projects/{projectKey}/statuses/{statusID}", { params: { path: { ...path, statusID: id } }, body })).data!.status,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/projects/{projectKey}/statuses/{statusID}", { params: { path: { ...path, statusID: id } } });
      },
      onSuccess: refresh,
    }),
  };
}

/** Makes, saves whole, deletes and assigns a project's workflows. */
export function useWorkflowActions(projectKey: string) {
  const refresh = useRefresh(projectKey);
  const path = { projectKey };
  return {
    create: useMutation({
      mutationFn: async (body: WorkflowInput): Promise<Workflow> =>
        (await api.POST("/projects/{projectKey}/workflows", { params: { path }, body })).data!.workflow,
      onSuccess: refresh,
    }),
    save: useMutation({
      mutationFn: async ({ id, ...body }: WorkflowInput & { id: string }): Promise<Workflow> =>
        (await api.PUT("/projects/{projectKey}/workflows/{workflowID}", { params: { path: { ...path, workflowID: id } }, body })).data!.workflow,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/projects/{projectKey}/workflows/{workflowID}", { params: { path: { ...path, workflowID: id } } });
      },
      onSuccess: refresh,
    }),
    assign: useMutation({
      mutationFn: async (body: WorkflowAssignment): Promise<Workflow> =>
        (await api.PUT("/projects/{projectKey}/workflow", { params: { path }, body })).data!.workflow,
      onSuccess: refresh,
    }),
  };
}

/** The transitions the reader may take on a requirement now; none for a section. */
export function useTransitions(reqKey: string, enabled = true) {
  return useQuery({
    queryKey: transitionsKey(reqKey),
    queryFn: async (): Promise<AvailableTransition[]> =>
      (await api.GET("/requirements/{reqKey}/transitions", { params: { path: { reqKey } } })).data!.transitions,
    enabled: Boolean(reqKey) && enabled,
  });
}

export type RequirementTransitions = Wire["RequirementTransitions"];
export type BulkTransitionResult = Wire["BulkTransitionResult"];

/** What the reader may take now on each of several requirements of a project; one they may not is left out. */
export function useBulkTransitions(projectKey: string, keys: string[]) {
  return useQuery({
    queryKey: ["transitions", "bulk", projectKey.toUpperCase(), keys] as const,
    queryFn: async (): Promise<RequirementTransitions[]> =>
      (await api.GET("/projects/{projectKey}/transitions", { params: { path: { projectKey }, query: { key: keys } } })).data!.requirements,
    enabled: keys.length > 0,
  });
}

/** Takes one transition on several requirements, each on its own; everything that shows a status is read anew. */
export function useTakeBulkTransition(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { transitionId: string; comment?: string; keys: string[] }): Promise<BulkTransitionResult[]> =>
      (await api.POST("/projects/{projectKey}/transitions", { params: { path: { projectKey } }, body })).data!.results,
    onSettled: () => Promise.all([["spec"], ["tree"], ["requirements"], ["transitions"]].map((queryKey) => queryClient.invalidateQueries({ queryKey }))),
  });
}

/** Takes a transition; the requirement, its history and what it offers next are read anew. */
export function useTakeTransition(reqKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: { transitionId: string; comment?: string }): Promise<Requirement> =>
      (await api.POST("/requirements/{reqKey}/transitions", { params: { path: { reqKey } }, body })).data!.requirement,
    onSuccess: (requirement) => {
      queryClient.setQueryData(requirementQueryKey(requirement.key), requirement);
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: ["requirements", "list"] }),
        queryClient.invalidateQueries({ queryKey: ["history", requirement.key] }),
        queryClient.invalidateQueries({ queryKey: transitionsKey(requirement.key) }),
      ]);
    },
    // A transition somebody else's move made stale offers what is left.
    onError: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: transitionsKey(reqKey) }),
        queryClient.invalidateQueries({ queryKey: requirementQueryKey(reqKey) }),
      ]),
  });
}
