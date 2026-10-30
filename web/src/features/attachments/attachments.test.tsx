import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { formatSize, isImage, showsInPlace } from "@/api/attachments";
import { answerConfirm, renderAt, stubApi, type Answer } from "@/test/app";
import { anAttachment } from "@/test/attachments";
import { axeViolations } from "@/test/axe";
import { aProject } from "@/test/projects";
import { aRequirement } from "@/test/requirements";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const picture = anAttachment({ referenced: true });
const spec = anAttachment({
  id: "0195f000-0000-7000-8000-0000000004a2",
  fileName: "spec.pdf",
  contentType: "application/pdf",
  width: null,
  height: null,
  size: 3_500_000,
});

function stubRequirement({
  edit = true,
  files = [picture, spec],
  requirement = aRequirement({ can: { edit, transition: edit, delete: edit, link: edit, comment: true } }),
  more = {},
}: {
  edit?: boolean;
  files?: ReturnType<typeof anAttachment>[];
  requirement?: ReturnType<typeof aRequirement>;
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  return stubApi({
    "GET /projects/CHK": { status: 200, body: { project: aProject() } },
    "GET /requirements/CHK-R1": { status: 200, body: { requirement } },
    "GET /requirements/CHK-R1/links": { status: 200, body: { links: [] } },
    "GET /requirements/CHK-R1/attachments": { status: 200, body: { attachments: files } },
    ...more,
  });
}

describe("files", () => {
  it("shows pictures and PDFs in place and downloads the rest, and reads sizes as people do", () => {
    expect(showsInPlace("image/png")).toBe(true);
    expect(showsInPlace("text/plain; charset=utf-8")).toBe(true);
    expect(showsInPlace("image/svg+xml")).toBe(false);
    expect(isImage("image/webp")).toBe(true);
    expect(isImage("application/pdf")).toBe(false);
    expect([formatSize(512), formatSize(2048), formatSize(3_500_000)]).toEqual(["512 B", "2.0 KB", "3.3 MB"]);
  });
});

describe("a requirement's files", () => {
  it("lists them for a reader, each to open or download, and offers nothing to change", async () => {
    stubRequirement({ edit: false });
    await renderAt("/p/CHK/r/CHK-R1");
    const panel = await screen.findByRole("region", { name: "2 files" });
    expect(within(panel).getByRole("link", { name: "Open basket.png" })).toHaveAttribute("href", `/api/v1/attachments/${picture.id}?inline=true`);
    expect(within(panel).getAllByRole("link", { name: "Download spec.pdf" })[0]).toHaveAttribute("download", "spec.pdf");
    expect(within(panel).getByText("Shown in the statement")).toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: "Attach files" })).not.toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: /Delete/ })).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("attaches the files picked, and says why one was refused", async () => {
    let sentFiles = 0;
    const sent = stubRequirement({
      files: [],
      more: {
        "GET /requirements/CHK-R1/attachments": () => ({ status: 200, body: { attachments: sentFiles > 0 ? [spec] : [] } }),
        // The files go up in the order picked; the second is the one over the limit.
        "POST /requirements/CHK-R1/attachments": () =>
          ++sentFiles === 1
            ? { status: 201, body: { attachment: spec } }
            : { status: 413, body: { error: { code: "too_large", message: "That file is too large: a requirement takes files of up to 50 MB." } } },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    expect(await screen.findByText("No files are attached. Attach one, or drop it here.")).toBeInTheDocument();
    const input = document.querySelector<HTMLInputElement>("[data-attachment-input]")!;
    await userEvent.upload(input, [new File(["%PDF"], "spec.pdf", { type: "application/pdf" }), new File(["x"], "big.bin")]);
    expect(await screen.findByText("Attached spec.pdf.")).toBeInTheDocument();
    expect(await screen.findByText("big.bin (1 B) was not attached. That file is too large: a requirement takes files of up to 50 MB.")).toBeInTheDocument();
    expect(sent.filter((each) => each.method === "POST" && each.path === "/requirements/CHK-R1/attachments")).toHaveLength(2);
    expect(await screen.findByRole("link", { name: "Open spec.pdf" })).toBeInTheDocument();
  });

  it("deletes a file once its editor agrees", async () => {
    const sent = stubRequirement({ more: { [`DELETE /attachments/${spec.id}`]: { status: 204 } } });
    await renderAt("/p/CHK/r/CHK-R1");
    const remove = await screen.findByRole("button", { name: "Delete spec.pdf" });
    await userEvent.click(remove);
    expect(await answerConfirm(false)).toHaveTextContent("Delete attachment spec.pdf?");
    expect(sent.some((each) => each.method === "DELETE")).toBe(false);
    await userEvent.click(remove);
    await answerConfirm(true);
    expect(await screen.findByText("Deleted spec.pdf.")).toBeInTheDocument();
    expect(sent.find((each) => each.method === "DELETE")?.path).toBe(`/attachments/${spec.id}`);
  });
});

describe("files in a statement", () => {
  it("draws a picture from the API and a file in a line as its download", async () => {
    const statement = {
      type: "doc",
      content: [
        { type: "image", attrs: { attachmentId: picture.id, alt: "The basket", width: 320 } },
        {
          type: "paragraph",
          content: [
            { type: "text", text: "See " },
            { type: "attachment", attrs: { attachmentId: spec.id, fileName: "spec.pdf" } },
          ],
        },
      ],
    };
    stubRequirement({ requirement: aRequirement({ statement }) });
    await renderAt("/p/CHK/r/CHK-R1");
    const shown = await screen.findByRole("img", { name: "The basket" });
    expect(shown).toHaveAttribute("src", `/api/v1/attachments/${picture.id}?inline=true`);
    expect(shown).toHaveStyle({ width: "320px" });
    const statementBox = document.querySelector("[data-statement]") as HTMLElement;
    expect(within(statementBox).getByRole("link", { name: "Download spec.pdf" })).toHaveAttribute("href", `/api/v1/attachments/${spec.id}`);
  });

  it("puts a picture uploaded from the editor into the statement it saves", async () => {
    const sent = stubRequirement({
      files: [],
      more: {
        "POST /requirements/CHK-R1/attachments": { status: 201, body: { attachment: picture } },
        "PATCH /requirements/CHK-R1": { status: 200, body: { requirement: aRequirement({ revision: 2 }) } },
      },
    });
    await renderAt("/p/CHK/r/CHK-R1");
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const editor = document.querySelector<HTMLElement>("#edit-statement")!;
    const input = editor.closest("[data-editor-frame]")!.querySelector<HTMLInputElement>("[data-editor-file-input]")!;
    await userEvent.upload(input, new File(["png"], "basket.png", { type: "image/png" }));
    await waitFor(() => expect(editor.querySelector(`figure[data-image][data-attachment-id="${picture.id}"]`)).not.toBeNull());
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PATCH")).toBe(true));
    const saved = sent.find((each) => each.method === "PATCH")?.body as { statement: { content: { type: string; attrs?: { attachmentId?: string } }[] } };
    expect(saved.statement.content.some((node) => node.type === "image" && node.attrs?.attachmentId === picture.id)).toBe(true);
  });
});
