import { expect, request } from "@playwright/test";
import { must, type ExciterApi } from "./api";
import { WEB_URL } from "./stack";

const RUN_LANDS = 30_000;

/** The name a passing result for the requirement carries, its tag naming what it verifies. */
export function passingTestName(reqKey: string): string {
  return `engage [verifies ${reqKey}]`;
}

/** Uploads a passing JUnit result for the requirement on main as a pipeline does, with an evidence token of its own
 *  that is let go after, and waits for the worker to land it. */
export async function uploadPassingResult(api: ExciterApi, projectKey: string, reqKey: string) {
  const token = must(await api.POST("/tokens", { body: { name: `ci ${projectKey}`, scopes: ["evidence"] } })).token;
  const ci = await request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { Authorization: `Bearer ${token.secret}` } });
  try {
    const report = `<?xml version="1.0"?><testsuites><testsuite name="brakes"><testcase classname="brakes" name="${passingTestName(reqKey)}" time="0.004"/></testsuite></testsuites>`;
    const made = await ci.post(`/api/v1/projects/${projectKey}/test-runs`, {
      multipart: {
        repository: "acme/brakes",
        commit: "4f2c9a1e7b3d",
        branch: "main",
        file: { name: "junit.xml", mimeType: "application/xml", buffer: Buffer.from(report) },
      },
    });
    expect(made.status()).toBe(201);
    const id = (await made.json()).run.id as string;
    await expect.poll(async () => (await (await ci.get(`/api/v1/test-runs/${id}`)).json()).run.state, { timeout: RUN_LANDS }).toBe("done");
  } finally {
    await ci.dispose();
    await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
  }
}
