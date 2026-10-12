import { request } from "@playwright/test";
import { mailpitURL } from "./stack";

/** One mail Mailpit caught. */
export interface CaughtMail {
  ID: string;
  Subject: string;
}

/** The mails caught for an address whose subject holds the words, the latest first. */
export async function mailsTo(address: string, subjectHas: string): Promise<CaughtMail[]> {
  const context = await request.newContext({ baseURL: mailpitURL() });
  try {
    const query = `to:${address} subject:"${subjectHas}"`;
    const res = await context.get(`/api/v1/search?query=${encodeURIComponent(query)}`);
    if (!res.ok()) throw new Error(`Mailpit answered ${res.status()} to a search: ${await res.text()}`);
    return ((await res.json()) as { messages: CaughtMail[] }).messages;
  } finally {
    await context.dispose();
  }
}

/** The plain text of one caught mail. */
export async function mailText(id: string): Promise<string> {
  const context = await request.newContext({ baseURL: mailpitURL() });
  try {
    const res = await context.get(`/api/v1/message/${encodeURIComponent(id)}`);
    if (!res.ok()) throw new Error(`Mailpit answered ${res.status()} for message ${id}`);
    return ((await res.json()) as { Text: string }).Text;
  } finally {
    await context.dispose();
  }
}
