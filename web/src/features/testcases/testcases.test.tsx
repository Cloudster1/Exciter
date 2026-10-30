import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { LONG_TEST_MS, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => vi.unstubAllGlobals());

const doc = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const typeId = "0195f000-0000-7000-8000-00000000c0a1";
const testType = {
  id: typeId,
  name: "Test case",
  description: "",
  icon: "requirement",
  colour: "#4f6bed",
  position: 1,
  workflowId: null,
  role: "testCase" as const,
  roleHeld: true,
  retired: false,
  requirements: 1,
  createdAt: "2026-10-18T08:00:00Z",
  updatedAt: "2026-10-18T08:00:00Z",
};
const brake = aRequirement({
  key: "CHK-R1",
  title: "Brake on a wet road",
  type: { id: typeId, name: "Test case", icon: "requirement", colour: "#4f6bed" },
  testCase: {
    preconditions: doc("The car stands on a wet road."),
    steps: [
      { action: doc("Drive at the speed."), expected: null },
      { action: doc("Press the pedal."), expected: doc("The car stops within 40 m.") },
    ],
    parameters: [{ name: "Speed", values: ["30", "50"] }],
  },
});
const stubs = {
  "GET /projects/CHK": { status: 200, body: { project: aProject() } },
  "GET /requirements/CHK-R1": { status: 200, body: { requirement: brake } },
  "GET /projects/CHK/types": { status: 200, body: { types: [testType] } },
  "GET /projects/CHK/attributes": { status: 200, body: { attributes: [] } },
};

describe("a test case", () => {
  it("shows its preconditions, its parameters by iteration and its steps", async () => {
    stubApi(stubs);
    await renderAt("/projects/CHK/r/CHK-R1");
    const steps = await screen.findByRole("table", { name: "Steps" });
    expect(within(steps).getByRole("row", { name: /2 Press the pedal\. The car stops within 40 m\./ })).toBeInTheDocument();
    expect(screen.getByText("The car stands on a wet road.")).toBeInTheDocument();
    const parameters = screen.getByRole("table", { name: "Parameters" });
    expect(within(parameters).getByRole("row", { name: "2 50" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it(
    "moves and removes steps, sets parameters, and sends the case whole",
    async () => {
      const sent = stubApi({ ...stubs, "PATCH /requirements/CHK-R1": { status: 200, body: { requirement: { ...brake, revision: 2 } } } });
      await renderAt("/projects/CHK/r/CHK-R1");
      await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
      const steps = await screen.findByRole("group", { name: "Steps" });
      await userEvent.click(within(steps).getByRole("button", { name: "Move step 2 up" }));
      await userEvent.click(within(steps).getByRole("button", { name: "Remove step 2" }));
      const parameters = screen.getByRole("group", { name: "Parameters" });
      const values = within(parameters).getByRole("textbox", { name: "Values, apart by semicolons" });
      // One paste rather than a key at a time, which the editor redraws for each.
      await userEvent.clear(values);
      await userEvent.click(values);
      await userEvent.paste("30; 50; 80");
      expect(await axeViolations()).toEqual([]);
      await userEvent.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(sent.some((r) => r.method === "PATCH")).toBe(true));
      const body = sent.find((r) => r.method === "PATCH")?.body as { testCase: { steps: { action: unknown }[]; parameters: unknown } };
      expect(body.testCase.steps).toEqual([{ action: doc("Press the pedal."), expected: doc("The car stops within 40 m.") }]);
      expect(body.testCase.parameters).toEqual([{ name: "Speed", values: ["30", "50", "80"] }]);
    },
    LONG_TEST_MS,
  );
});
