import { QueryObserver } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { createQueryClient } from "./session";

/** A list the server answers by hand, one request at a time, as it stood when each was asked. */
function server() {
  let apps: string[] = [];
  const answers: Array<() => void> = [];
  let asked = 0;
  return {
    get asked() {
      return asked;
    },
    add(app: string) {
      apps = [...apps, app];
    },
    list(): Promise<string[]> {
      asked++;
      const seen = apps;
      return new Promise((resolve) => answers.push(() => resolve(seen)));
    },
    answerAll() {
      answers.splice(0).forEach((answer) => answer());
    },
  };
}

async function settled() {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

describe("the query client", () => {
  it("asks a list again when something is saved while its first load is still on the way (#372)", async () => {
    const client = createQueryClient(() => {});
    const api = server();
    const observer = new QueryObserver(client, { queryKey: ["partner-apps"], queryFn: () => api.list() });
    const stop = observer.subscribe(() => {});

    expect(api.asked).toBe(1);
    api.add("Wiki");
    const invalidated = client.invalidateQueries({ queryKey: ["partner-apps"] });
    await settled();
    api.answerAll();
    await invalidated;
    await settled();

    expect(api.asked).toBe(2);
    expect(client.getQueryData(["partner-apps"])).toEqual(["Wiki"]);
    stop();
  });

  it("asks a list that has loaded once more, and leaves a list nobody shows alone", async () => {
    const client = createQueryClient(() => {});
    const api = server();
    const observer = new QueryObserver(client, { queryKey: ["partner-apps"], queryFn: () => api.list() });
    const stop = observer.subscribe(() => {});
    api.answerAll();
    await settled();

    api.add("Wiki");
    const invalidated = client.invalidateQueries({ queryKey: ["partner-apps"] });
    await settled();
    api.answerAll();
    await invalidated;
    expect(api.asked).toBe(2);
    expect(client.getQueryData(["partner-apps"])).toEqual(["Wiki"]);

    stop();
    await client.invalidateQueries({ queryKey: ["partner-apps"] });
    expect(api.asked).toBe(2);
  });
});
