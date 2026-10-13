import type { Attachment } from "@/api/attachments";

/** A file as the API answers it: a picture on CHK-R1 unless a test says otherwise. */
export function anAttachment(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: "0195f000-0000-7000-8000-0000000004a1",
    requirementId: "0195f000-0000-7000-8000-0000000000a1",
    fileName: "basket.png",
    contentType: "image/png",
    size: 2048,
    width: 640,
    height: 480,
    uploadedByName: "Ada Admin",
    createdAt: "2026-10-01T08:00:00Z",
    referenced: false,
    ...overrides,
  };
}
