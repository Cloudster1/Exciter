import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Comment, Thread } from "@/api/comments";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const threadId = "0195f000-0000-7000-8000-00000000c0a1";
const replyId = "0195f000-0000-7000-8000-00000000c0a2";
const otherThreadId = "0195f000-0000-7000-8000-00000000c0b1";
const say = (text: string): Doc => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

function aComment(over: Partial<Comment> = {}): Comment {
  return {
    id: threadId,
    threadId,
    authorId: "0195f000-0000-7000-8000-0000000000e1",
    authorName: "Grace Hopper",
    revision: 1,
    body: say("Why 100 ms?"),
    deleted: false,
    createdAt: "2026-10-01T08:00:00Z",
    editedAt: null,
    can: { edit: false, delete: false },
    ...over,
  };
}

function aThread(over: Partial<Thread> = {}): Thread {
  return {
    id: threadId,
    requirementId: "0195f000-0000-7000-8000-0000000000a1",
    resolved: false,
    resolvedByName: "",
    resolvedAt: null,
    comments: [aComment()],
    can: { reply: true, resolve: true },
    ...over,
  };
}

const discussion = (): Thread[] => [
  aThread({
    comments: [
      aComment({ deleted: true, body: null }),
      aComment({
        id: replyId,
        authorId: signedIn.user.id,
        authorName: "Ada Lovelace",
        revision: 2,
        body: say("Because of the new sensor."),
        editedAt: "2026-10-01T09:00:00Z",
        can: { edit: true, delete: true },
      }),
    ],
  }),
  aThread({
    id: otherThreadId,
    resolved: true,
    resolvedByName: "Ada Lovelace",
    resolvedAt: "2026-10-01T10:00:00Z",
    comments: [aComment({ id: otherThreadId, threadId: otherThreadId, body: say("Who tests it?") })],
  }),
];

function stubPage({ comment = true, threads = discussion(), more = {} }: { comment?: boolean; threads?: Thread[]; more?: Record<string, Answer> } = {}) {
  const requirement = aRequirement({
    can: { edit: comment, transition: comment, delete: comment, link: comment, comment },
    comments: { threads: threads.length, open: threads.filter((each) => !each.resolved).length },
  });
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /requirements/CHK-R1/comments": { status: 200, body: { threads } },
    "GET /requirements/CHK-R1/mentionable": { status: 200, body: { people: [] } },
    ...more,
  });
}

/** Types into a comment editor once it has arrived. */
async function write(id: string, text: string) {
  const box = await waitFor(() => {
    const found = document.getElementById(id);
    if (!found) throw new Error(`no editor ${id} yet`);
    return found;
  });
  await userEvent.click(box);
  await userEvent.type(box, text);
  return box;
}

const section = () => screen.findByRole("region", { name: /Discussion/ });

describe("the discussion on a requirement", () => {
  it("lists threads with their replies and revisions, a placeholder for what was deleted, and what each allows", async () => {
    stubPage();
    await renderAt("/p/CHK/r/CHK-R1");
    const threads = await within(await section()).findAllByRole("article");
    expect(threads).toHaveLength(2);
    const first = threads[0]!;
    expect(first).toHaveAccessibleName("Thread started by Grace Hopper");
    expect(within(first).getByText("This comment was deleted.")).toBeInTheDocument();
    expect(within(first).getByText("Because of the new sensor.")).toBeInTheDocument();
    expect(within(first).getByText("· on revision 2")).toBeInTheDocument();
    expect(within(first).getByText(/edited/)).toBeInTheDocument();
    const mine = first.querySelector(`[data-comment="${replyId}"]`) as HTMLElement;
    expect(within(mine).getByRole("button", { name: "Edit" })).toBeInTheDocument();
    expect(within(first.querySelector(`[data-comment="${threadId}"]`) as HTMLElement).queryByRole("button", { name: "Edit" })).toBeNull();
    expect(within(threads[1]!).getByText("Resolved by Ada Lovelace")).toBeInTheDocument();
    expect(within(threads[1]!).getByRole("button", { name: "Reopen" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("starts a thread with the comment editor, which offers no rules, tables or files", async () => {
    const sent = stubPage({ threads: [], more: { "POST /requirements/CHK-R1/comments": { status: 201, body: { thread: aThread() } } } });
    await renderAt("/p/CHK/r/CHK-R1");
    expect(await within(await section()).findByText("Nobody has said anything about this requirement yet.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Comment" }));
    const box = await write("new-comment", "Looks right");
    expect(box).toHaveAccessibleName("Your comment");
    const toolbar = within(await section()).getByRole("toolbar");
    for (const gone of [t.editor.table, t.editor.rule, t.attachments.insert]) {
      expect(within(toolbar).queryByRole("button", { name: gone })).toBeNull();
    }
    expect(within(toolbar).getByRole("button", { name: t.editor.bulletList })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Post comment" }));
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === "/requirements/CHK-R1/comments")).toBe(true));
    const posted = sent.find((each) => each.method === "POST" && each.path === "/requirements/CHK-R1/comments")!.body as { body: Doc };
    expect(JSON.stringify(posted.body)).toContain("Looks right");
    await waitFor(() => expect(document.getElementById("new-comment")).toBeNull());
  });

  it("replies, resolves, edits and deletes one's own", async () => {
    const sent = stubPage({
      more: {
        [`POST /comments/${threadId}/replies`]: { status: 201, body: { comment: aComment(), thread: aThread() } },
        [`PUT /comment-threads/${threadId}/resolution`]: { status: 200, body: { thread: aThread({ resolved: true }) } },
        [`PATCH /comments/${replyId}`]: { status: 200, body: { comment: aComment({ id: replyId }) } },
        [`DELETE /comments/${replyId}`]: { status: 204 },
      },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    await renderAt("/p/CHK/r/CHK-R1");
    const first = (await within(await section()).findAllByRole("article"))[0]!;
    await userEvent.click(within(first).getByRole("button", { name: "Reply" }));
    expect(await write(`reply-${threadId}`, "Agreed")).toHaveAccessibleName("Your reply to Grace Hopper");
    await userEvent.click(within(first).getByRole("button", { name: "Post reply" }));
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === `/comments/${threadId}/replies`)).toBe(true));
    await userEvent.click(await within(first).findByRole("button", { name: "Resolve" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PUT" && each.path === `/comment-threads/${threadId}/resolution`)).toBe(true));

    const mine = first.querySelector(`[data-comment="${replyId}"]`) as HTMLElement;
    await userEvent.click(within(mine).getByRole("button", { name: "Edit" }));
    await write(`edit-${replyId}`, " still");
    await userEvent.click(within(mine).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PATCH" && each.path === `/comments/${replyId}`)).toBe(true));
    await userEvent.click(await within(mine).findByRole("button", { name: "Delete" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/comments/${replyId}`)).toBe(true));
    expect(window.confirm).toHaveBeenCalledWith("Delete your comment? Its place in the thread stays.");
  });

  it("tells a reader they may follow but not take part", async () => {
    stubPage({ comment: false, threads: [aThread({ can: { reply: false, resolve: false } })] });
    await renderAt("/p/CHK/r/CHK-R1");
    const region = await section();
    expect(await within(region).findByText(/You may read this discussion but not take part/)).toBeInTheDocument();
    expect(within(region).queryByRole("button", { name: "Comment" })).toBeNull();
    expect(within(region).queryByRole("button", { name: "Reply" })).toBeNull();
  });
});
