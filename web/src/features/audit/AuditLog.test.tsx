import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { auditExportHref, auditQuery, type AuditEntry } from "@/api/audit";
import { AUDIT_PATH } from "@/config";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { describe as describeEntry, targetName } from "./AuditLog";

afterEach(() => vi.unstubAllGlobals());

const ada = signedIn.user.id;
const bobId = "0199a000-0000-7000-8000-0000000000b0";
const eventId = "0199a000-0000-7000-8000-0000000000e0";

const entry = (over: Partial<AuditEntry>): AuditEntry => ({
  id: crypto.randomUUID(),
  action: "member.created",
  targetType: "user",
  targetId: bobId,
  actorId: ada,
  actorName: "Ada Lovelace",
  data: {},
  ip: "",
  createdAt: "2026-09-30T08:00:00Z",
  ...over,
});

const first = [
  entry({ action: "member.role_changed", data: { name: "Bob Builder", from: "member", to: "admin" }, ip: "10.0.0.7" }),
  entry({ action: "event.dead_lettered", targetType: "event", targetId: eventId, actorId: null, actorName: "", data: { topic: "member.created" } }),
  entry({ action: "member.removed", actorId: "0199a000-0000-7000-8000-0000000000c0", actorName: "", data: { role: "guest" } }),
  entry({ action: "member.created", data: { role: "member", newAccount: true } }),
];
const second = [entry({ action: "audit.exported", targetType: "audit_log", targetId: null, data: { rows: 12 } })];

const facets = {
  facets: {
    actions: ["audit.exported", "event.dead_lettered", "member.created", "member.removed", "member.role_changed"],
    actors: [{ id: ada, name: "Ada Lovelace" }],
    targetTypes: ["audit_log", "event", "user"],
    retentionDays: 365,
  },
};

/** The API's log, a page of four then a page of one, recording each query it was asked. */
function log(extra: Record<string, Answer | ((request: Request) => Answer)> = {}) {
  const queries: URLSearchParams[] = [];
  stubApi({
    "GET /audit/facets": { status: 200, body: facets },
    "GET /audit": (request) => {
      const query = new URL(request.url).searchParams;
      queries.push(query);
      return query.get("cursor") === "page-2"
        ? { status: 200, body: { entries: second, next: null } }
        : { status: 200, body: { entries: first, next: "page-2" } };
    },
    ...extra,
  });
  return queries;
}

describe("the audit log", () => {
  it("lists who did what to what, newest first, in words", async () => {
    log();
    await renderAt(AUDIT_PATH);
    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(first.length + 1);
    expect(rows[1]).toHaveTextContent("Role changed");
    expect(rows[1]).toHaveTextContent("member.role_changed");
    expect(rows[1]).toHaveTextContent("Bob Builder");
    expect(rows[1]).toHaveTextContent("from: member; to: admin");
    expect(rows[1]).toHaveTextContent("from 10.0.0.7");
    expect(rows[2]).toHaveTextContent("System");
    expect(rows[3]).toHaveTextContent("Former member");
    expect(screen.getByText("Each entry is kept for 365 days.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("narrows by action, person, kind and the reader's own days", async () => {
    const queries = log();
    await renderAt(AUDIT_PATH);
    await screen.findByRole("table");
    await userEvent.selectOptions(screen.getByLabelText("Action"), "member.created");
    await waitFor(() => expect(queries.at(-1)?.get("action")).toBe("member.created"));
    await userEvent.selectOptions(screen.getByLabelText("Person"), ada);
    await waitFor(() => expect(queries.at(-1)?.get("actor")).toBe(ada));
    await userEvent.selectOptions(screen.getByLabelText("Kind of target"), "user");
    await waitFor(() => expect(queries.at(-1)?.get("targetType")).toBe("user"));
    await userEvent.type(screen.getByLabelText("From"), "2026-09-01");
    await waitFor(() => expect(queries.at(-1)?.get("from")).toBe(new Date(2026, 8, 1).toISOString()));
    expect(screen.getByRole("link", { name: "Export CSV" }).getAttribute("href")).toContain("action=member.created");

    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(screen.getByLabelText("Action")).toHaveValue("");
    expect(screen.getByLabelText("From")).toHaveValue("");
    expect(screen.getByRole("link", { name: "Export CSV" })).toHaveAttribute("href", "/api/v1/audit/export");
    expect(within(await screen.findByRole("table")).getAllByRole("row")).toHaveLength(first.length + 1);
  });

  it("shows only one target's entries when its name is followed, until told otherwise", async () => {
    const queries = log();
    await renderAt(AUDIT_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Show only entries about Bob Builder" }));
    await waitFor(() => expect(queries.at(-1)?.get("target")).toBe(bobId));
    expect(screen.getByText("Only entries about Bob Builder")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Export CSV" }).getAttribute("href")).toContain(`target=${bobId}`);
    await userEvent.click(screen.getByRole("button", { name: "Show every target" }));
    expect(screen.queryByText("Only entries about Bob Builder")).toBeNull();
    expect(screen.getByRole("link", { name: "Export CSV" }).getAttribute("href")).not.toContain("target=");
  });

  it("pages older and back newer by the cursor the API handed out", async () => {
    const queries = log();
    await renderAt(AUDIT_PATH);
    await screen.findByRole("table");
    expect(screen.getByText("Page 1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Newer" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Older" }));
    await waitFor(() => expect(queries.at(-1)?.get("cursor")).toBe("page-2"));
    await waitFor(() => expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(second.length + 1));
    expect(within(screen.getByRole("table")).getByText("Audit log exported")).toBeInTheDocument();
    expect(screen.getByText("Page 2")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Older" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Newer" }));
    expect(await screen.findByText("Page 1")).toBeInTheDocument();
  });

  it("puts a range the server refuses beside the field", async () => {
    log({
      "GET /audit": {
        status: 422,
        body: {
          error: { code: "validation_failed", message: "Some fields need attention.", fields: { to: "The end of the range has to come after its start." } },
        },
      },
    });
    await renderAt(AUDIT_PATH);
    expect(await screen.findByText("The end of the range has to come after its start.")).toBeInTheDocument();
    expect(screen.getByLabelText("To")).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("tells a member it is the administrators' to read", async () => {
    const sent = log();
    await renderAt(AUDIT_PATH, { me: { ...signedIn, organization: { ...signedIn.organization!, role: "member" } } });
    expect(await screen.findByText(/Only an owner or administrator of this organization reads its audit log/)).toBeInTheDocument();
    expect(sent).toHaveLength(0);
  });
});

describe("an entry in words", () => {
  it("is known by the name it kept, else its id, and its details leave ids out", () => {
    expect(targetName(first[0]!)).toBe("Bob Builder");
    expect(targetName(entry({ data: {} }))).toBe(bobId);
    const details = describeEntry(
      entry({ data: { key: "CHK", authorId: "x", grants: [{ subject: "a" }, { subject: "b" }], scopes: ["read"] }, ip: "10.0.0.7" }),
    );
    expect(details).toBe("key: CHK; grants: 2; scopes: read; from 10.0.0.7");
  });

  it("asks for the reader's days as the instants they span", () => {
    const query = auditQuery({ from: "2026-09-01", to: "2026-09-30" });
    expect(query.from).toBe(new Date(2026, 8, 1).toISOString());
    expect(query.to).toBe(new Date(2026, 9, 1).toISOString());
    expect(auditExportHref({})).toBe("/api/v1/audit/export");
    expect(auditExportHref({ action: "member.removed" })).toBe("/api/v1/audit/export?action=member.removed");
  });
});
