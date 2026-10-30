import { afterEach, describe, expect, it, vi } from "vitest";
import { renderHook, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useMentionSource, type Comment, type Thread } from "@/api/comments";
import type { Project } from "@/api/projects";
import type { Requirement } from "@/api/requirements";
import { answerConfirm, renderAt, stubApi, type Answer } from "@/test/app";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";

// The edges of the discussion (#312): cancelling, a failed post, moderation, reopening, a closed discussion and
// whom the mention picker offers.

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const threadId = "0195f000-0000-7000-8000-00000000c0a1";
const say = (text: string): Doc => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

function aComment(over: Partial<Comment> = {}): Comment {
  return {
    id: threadId,
    via: null,
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

function stubPage({
  requirement = aRequirement(),
  project = aProject(),
  threads = [],
  more = {},
}: {
  requirement?: Requirement;
  project?: Project;
  threads?: Thread[];
  more?: Record<string, Answer>;
} = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement: { ...requirement, comments: { threads: threads.length, open: threads.length } } } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /requirements/CHK-R1/comments": { status: 200, body: { threads } },
    "GET /requirements/CHK-R1/mentionable": { status: 200, body: { people: [] } },
    ...more,
  });
}

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
const posted = (sent: { method: string; path: string }[]) => sent.filter((each) => each.method !== "GET");

describe("the discussion's edges", () => {
  it("cancels a new comment without sending anything, and offers to comment again", async () => {
    const sent = stubPage();
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await within(await section()).findByRole("button", { name: "Comment" }));
    await write("new-comment", "Second thoughts");
    await userEvent.click(within(await section()).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(document.getElementById("new-comment")).toBeNull());
    expect(within(await section()).getByRole("button", { name: "Comment" })).toBeInTheDocument();
    expect(posted(sent)).toEqual([]);
  });

  it("keeps the words of a post that failed and says why", async () => {
    stubPage({
      more: {
        "POST /requirements/CHK-R1/comments": { status: 500, body: { error: { code: "internal", message: "The discussion is down. Try again." } } },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await within(await section()).findByRole("button", { name: "Comment" }));
    await write("new-comment", "Worth keeping");
    await userEvent.click(within(await section()).getByRole("button", { name: "Post comment" }));
    expect(await within(await section()).findByText("The discussion is down. Try again.")).toBeInTheDocument();
    expect(document.getElementById("new-comment")).toHaveTextContent("Worth keeping");
  });

  it("asks before an administrator deletes somebody else's comment, naming them", async () => {
    const sent = stubPage({
      threads: [aThread({ comments: [aComment({ can: { edit: false, delete: true } })] })],
      more: { [`DELETE /comments/${threadId}`]: { status: 204 } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    const thread = (await within(await section()).findAllByRole("article"))[0]!;
    await userEvent.click(within(thread).getByRole("button", { name: "Delete" }));
    const asked = await answerConfirm(true);
    expect(asked).toHaveTextContent("Delete Grace Hopper's comment?");
    expect(asked).toHaveTextContent(t.comments.confirmDeleteOthers);
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/comments/${threadId}`)).toBe(true));
  });

  it("sends nothing when the deletion is not confirmed", async () => {
    const sent = stubPage({ threads: [aThread({ comments: [aComment({ can: { edit: false, delete: true } })] })] });
    await renderAt("/p/CHK/r/CHK-R1");
    const thread = (await within(await section()).findAllByRole("article"))[0]!;
    await userEvent.click(within(thread).getByRole("button", { name: "Delete" }));
    await answerConfirm(false);
    expect(posted(sent)).toEqual([]);
  });

  it("reopens a resolved thread", async () => {
    const sent = stubPage({
      threads: [aThread({ resolved: true, resolvedByName: "Grace Hopper", resolvedAt: "2026-10-01T09:00:00Z" })],
      more: { [`DELETE /comment-threads/${threadId}/resolution`]: { status: 200, body: { thread: aThread() } } },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await within(await section()).findByRole("button", { name: "Reopen" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/comment-threads/${threadId}/resolution`)).toBe(true));
  });

  it.each([
    ["a requirement in the trash", { requirement: aRequirement({ trashedAt: "2026-10-02T08:00:00Z", can: { ...aRequirement().can, comment: false } }) }],
    [
      "an archived project",
      { requirement: aRequirement({ can: { ...aRequirement().can, comment: false } }), project: aProject({ archivedAt: "2026-10-02T08:00:00Z" }) },
    ],
  ])("says the discussion is closed for %s, and offers no comment", async (_, page) => {
    stubPage({ ...page, threads: [aThread({ can: { reply: false, resolve: false } })] });
    await renderAt("/p/CHK/r/CHK-R1");
    const region = await section();
    expect(await within(region).findByText(t.comments.closed)).toBeInTheDocument();
    expect(within(region).queryByText(t.comments.readOnly)).toBeNull();
    expect(within(region).queryByRole("button", { name: "Comment" })).toBeNull();
    expect(within(region).queryByRole("button", { name: "Reply" })).toBeNull();
  });
});

describe("the mention picker's people", () => {
  it("leaves out whoever may not read the requirement", async () => {
    stubApi({
      "GET /requirements/CHK-R1/mentionable": {
        status: 200,
        body: {
          people: [
            { id: "0195f000-0000-7000-8000-0000000000b1", name: "Carol Clerk", email: "carol@exciter.test", canView: true },
            { id: "0195f000-0000-7000-8000-0000000000b2", name: "Carl Outsider", email: "carl@exciter.test", canView: false },
          ],
        },
      },
    });
    const { result } = renderHook(() => useMentionSource("CHK-R1"));
    expect(await result.current("car")).toEqual([{ id: "0195f000-0000-7000-8000-0000000000b1", name: "Carol Clerk", email: "carol@exciter.test" }]);
  });
});
