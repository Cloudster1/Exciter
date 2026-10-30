import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { API_BASE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

// Test plans and test executions (#251): a plan groups test cases, an execution runs them and its testers record.

type Wire = components["schemas"];

export type TestPlan = Wire["TestPlan"];
export type TestPlanInput = Wire["TestPlanInput"];
export type TestExecution = Wire["TestExecution"];
export type TestExecutionInput = Wire["TestExecutionInput"];
export type ExecutionCase = Wire["TestExecutionCase"];
export type CaseRun = Wire["TestCaseRun"];
export type RecordInput = Wire["TestRecordInput"];
export type Outcome = NonNullable<CaseRun["outcome"]>;
export type ExecutionState = TestExecution["state"];
export type ExecutionReport = Wire["TestExecutionReport"];
export type CaseResult = Wire["TestCaseResult"];
export type LastRun = Wire["TestCaseLastRun"];

/** The outcomes a step or a case comes to, in the order a runner offers them. */
export const OUTCOMES: Outcome[] = ["passed", "failed", "blocked", "skipped"];

const testsKey = ["tests"] as const;
const plansKey = (projectKey: string) => [...testsKey, "plans", projectKey.toUpperCase()] as const;
const executionKey = (id: string) => [...testsKey, "execution", id] as const;

export function useTestPlans(projectKey: string) {
  return useQuery({
    queryKey: plansKey(projectKey),
    queryFn: async (): Promise<TestPlan[]> => (await api.GET("/projects/{projectKey}/test-plans", { params: { path: { projectKey } } })).data!.plans,
  });
}

export function useTestPlan(id: string) {
  return useQuery({
    queryKey: [...testsKey, "plan", id],
    queryFn: async (): Promise<TestPlan> => (await api.GET("/test-plans/{planID}", { params: { path: { planID: id } } })).data!.plan,
  });
}

/** Makes, changes and deletes a project's plans, every list of them read again. */
export function useTestPlanActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: testsKey });
  return {
    create: useMutation({
      mutationFn: async (body: TestPlanInput): Promise<TestPlan> =>
        (await api.POST("/projects/{projectKey}/test-plans", { params: { path: { projectKey } }, body })).data!.plan,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: Wire["TestPlanPatch"] & { id: string }): Promise<TestPlan> =>
        (await api.PATCH("/test-plans/{planID}", { params: { path: { planID: id } }, body })).data!.plan,
      onSuccess: refresh,
    }),
    setCases: useMutation({
      mutationFn: async ({ id, cases }: { id: string; cases: string[] }): Promise<TestPlan> =>
        (await api.PUT("/test-plans/{planID}/cases", { params: { path: { planID: id } }, body: { cases } })).data!.plan,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/test-plans/{planID}", { params: { path: { planID: id } } });
      },
      onSuccess: refresh,
    }),
  };
}

export function useTestExecutions(projectKey: string, state?: ExecutionState) {
  return useQuery({
    queryKey: [...testsKey, "executions", projectKey.toUpperCase(), state ?? ""],
    queryFn: async (): Promise<TestExecution[]> =>
      (await api.GET("/projects/{projectKey}/test-executions", { params: { path: { projectKey }, query: state ? { state } : {} } })).data!.executions,
  });
}

export function useMyTestExecutions() {
  return useQuery({
    queryKey: [...testsKey, "mine"],
    queryFn: async (): Promise<TestExecution[]> => (await api.GET("/test-executions/mine")).data!.executions,
  });
}

export function useTestExecution(id: string) {
  return useQuery({
    queryKey: executionKey(id),
    queryFn: async (): Promise<TestExecution> => (await api.GET("/test-executions/{executionID}", { params: { path: { executionID: id } } })).data!.execution,
  });
}

/** Starts, changes and closes executions, every list of them and their cases read again. */
export function useExecutionActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: testsKey });
  return {
    start: useMutation({
      mutationFn: async (body: TestExecutionInput): Promise<TestExecution> =>
        (await api.POST("/projects/{projectKey}/test-executions", { params: { path: { projectKey } }, body })).data!.execution,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: Wire["TestExecutionPatch"] & { id: string }): Promise<TestExecution> =>
        (await api.PATCH("/test-executions/{executionID}", { params: { path: { executionID: id } }, body })).data!.execution,
      onSuccess: refresh,
    }),
    close: useMutation({
      mutationFn: async ({ id, ...body }: Wire["TestExecutionCloseInput"] & { id: string }): Promise<TestExecution> =>
        (await api.POST("/test-executions/{executionID}/close", { params: { path: { executionID: id } }, body })).data!.execution,
      onSuccess: refresh,
    }),
  };
}

export function useExecutionCase(execution: string, key: string, iteration: number) {
  return useQuery({
    queryKey: [...testsKey, "case", execution, key, iteration],
    queryFn: async (): Promise<ExecutionCase> =>
      (
        await api.GET("/test-executions/{executionID}/cases/{reqKey}", {
          params: { path: { executionID: execution, reqKey: key }, query: { iteration } },
        })
      ).data!.case,
  });
}

// A multipart body in the generated types is an object of strings; the one sent is the form itself.
function fileForm(file: File): { file: string } {
  const form = new FormData();
  form.append("file", file, file.name);
  return form as unknown as { file: string };
}

/** Records a case iteration, puts files on it and files its failure, its case and execution read again. */
export function useCaseActions(execution: string, key: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: testsKey });
  const path = { executionID: execution, reqKey: key };
  return {
    record: useMutation({
      mutationFn: async (body: RecordInput): Promise<ExecutionCase> =>
        (await api.PUT("/test-executions/{executionID}/cases/{reqKey}/result", { params: { path }, body })).data!.case,
      onSuccess: refresh,
    }),
    upload: useMutation({
      mutationFn: async ({ file, iteration, step }: { file: File; iteration: number; step?: number }) =>
        (
          await api.POST("/test-executions/{executionID}/cases/{reqKey}/files", {
            params: { path, query: { iteration, ...(step ? { step } : {}) } },
            body: fileForm(file),
          })
        ).data!.file,
      onSuccess: refresh,
    }),
    removeFile: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/test-execution-files/{fileID}", { params: { path: { fileID: id } } });
      },
      onSuccess: refresh,
    }),
    fileIssue: useMutation({
      mutationFn: async (body: Wire["TestFailureIssueInput"]) =>
        (await api.POST("/test-executions/{executionID}/cases/{reqKey}/issues", { params: { path }, body })).data!.issue,
      onSuccess: refresh,
    }),
  };
}

export function useTestCaseResults(key: string, enabled = true) {
  return useQuery({
    queryKey: [...testsKey, "results", key],
    enabled,
    queryFn: async (): Promise<CaseResult[]> => (await api.GET("/requirements/{reqKey}/test-executions", { params: { path: { reqKey: key } } })).data!.results,
  });
}

export function useExecutionReport(id: string) {
  return useQuery({
    queryKey: [...testsKey, "report", id],
    queryFn: async (): Promise<ExecutionReport> =>
      (await api.GET("/test-executions/{executionID}/report", { params: { path: { executionID: id } } })).data!.report,
  });
}

/** Where a file recorded in an execution downloads. */
export function executionFileHref(id: string): string {
  return `${API_BASE}/test-execution-files/${encodeURIComponent(id)}`;
}

/** Where an execution's report downloads in a format. */
export function executionReportHref(id: string, format: "pdf" | "docx"): string {
  return `${API_BASE}/test-executions/${encodeURIComponent(id)}/report.${format}`;
}
