import { describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { ArmatureIssue, ArmatureIssues, ArmatureStatus } from "@/api/armature";
import { ARMATURE_ACCOUNT_PATH } from "@/config";
import { IssueChip, chipState, type ChipState } from "./IssueChip";

const BASE = "https://armature.example.com";
const issue: ArmatureIssue = {
  key: "CP-1",
  summary: "Set up the build pipeline",
  url: `${BASE}/issues/CP-1`,
  type: "Task",
  status: { name: "In progress", category: "in_progress" },
  assigneeName: "",
  updatedAt: "2026-10-01T09:00:00Z",
};

function lookup(status: ArmatureStatus | undefined, issues: [string, ArmatureIssue | null][] = []): ArmatureIssues {
  return { status, issues: new Map(issues) };
}

function chip(state: ChipState, storedCategory = "", stale = false) {
  render(<IssueChip issueKey="CP-1" href={`${BASE}/issues/CP-1`} state={state} storedCategory={storedCategory} stale={stale} />);
  return document.querySelector('[data-armature-issue="CP-1"]')!;
}

const SENTENCES = [
  "Connect your Armature account to see this issue",
  "Not available to you in Armature",
  "Armature did not answer",
  "Armature needs an update to show this issue",
];

describe("what a chip can show", () => {
  it("asks to connect whenever the viewer's token cannot be used", () => {
    expect(chipState("CP-1", lookup("ok", [["CP-1", issue]]), false)).toEqual({ kind: "connect" });
    for (const status of ["not_configured", "not_connected", "rejected"] as const) {
      expect(chipState("CP-1", lookup(status), true), status).toEqual({ kind: "connect" });
    }
  });

  it("says why Armature could not be asked, waits for a key not answered yet and hides one Armature withholds", () => {
    expect(chipState("CP-1", lookup("unreachable"), true)).toEqual({ kind: "unreachable" });
    expect(chipState("CP-1", lookup("outdated"), true)).toEqual({ kind: "outdated" });
    expect(chipState("CP-1", lookup("ok", [["CP-2", issue]]), true)).toEqual({ kind: "loading" });
    expect(chipState("CP-1", lookup(undefined), true)).toEqual({ kind: "loading" });
    expect(chipState("CP-1", lookup("ok", [["CP-1", null]]), true)).toEqual({ kind: "hidden" });
    expect(chipState("CP-1", lookup("ok", [["CP-1", issue]]), true)).toEqual({ kind: "issue", issue });
  });
});

describe("a chip", () => {
  it("shows the issue live, as a link to it in Armature", () => {
    const shown = chip({ kind: "issue", issue }, "done", true);
    expect(shown).toHaveAttribute("href", issue.url);
    expect(shown).toHaveTextContent("CP-1");
    expect(shown).toHaveTextContent("Set up the build pipeline");
    expect(shown).toHaveTextContent("In progress");
    expect(shown).toHaveTextContent("Unassigned");
    // Live, the issue's own status is shown, and the stored one is not second-guessed.
    expect(shown.querySelector("[data-status-stale]")).toBeNull();
  });

  it("links the connect hint to the Armature account page", () => {
    const shown = chip({ kind: "connect" }, "todo");
    const hint = shown.querySelector('[data-action="connect-armature-hint"]');
    expect(hint).toHaveAttribute("href", ARMATURE_ACCOUNT_PATH);
    expect(hint).toHaveTextContent(SENTENCES[0]!);
    expect(screen.getByRole("link", { name: "CP-1" })).toHaveAttribute("href", `${BASE}/issues/CP-1`);
    expect(shown.querySelector('[data-status-category="todo"]')).toHaveTextContent("To do");
  });

  for (const [kind, sentence] of [
    ["hidden", SENTENCES[1]],
    ["unreachable", SENTENCES[2]],
    ["outdated", SENTENCES[3]],
  ] as const) {
    it(`says only why it cannot show the issue when ${kind}`, () => {
      const shown = chip({ kind });
      expect(shown).toHaveAttribute("data-state", kind);
      expect(shown).toHaveTextContent(sentence);
      for (const other of SENTENCES.filter((s) => s !== sentence)) expect(shown).not.toHaveTextContent(other);
      expect(shown.querySelector("[data-status-category]")).toBeNull();
    });
  }

  it("shows the key alone while the key is looked up", () => {
    const shown = chip({ kind: "loading" });
    expect(shown).toHaveTextContent(/^CP-1$/);
  });

  it("says the issue moved since only with a stored category that is stale", () => {
    expect(chip({ kind: "loading" }, "in_progress", true).querySelector("[data-status-stale]")).toHaveTextContent("Moved in Armature since");
    cleanup();
    expect(chip({ kind: "loading" }, "in_progress", false).querySelector("[data-status-stale]")).toBeNull();
    cleanup();
    expect(chip({ kind: "loading" }, "", true).querySelector("[data-status-stale]")).toBeNull();
  });
});
