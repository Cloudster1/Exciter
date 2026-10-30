import { describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { MentionTextarea, mentionQuery } from "./MentionTextarea";

const people = [
  { id: "1", name: "Ada Lovelace", email: "ada@exciter.test" },
  { id: "2", name: "Adam Smith", email: "adam@exciter.test" },
  { id: "3", name: "Grace Hopper", email: "grace@exciter.test" },
];

describe("mentionQuery", () => {
  it("reads the word after the last at sign before the caret", () => {
    expect(mentionQuery("ping @Ad", 8)).toEqual({ start: 5, query: "Ad" });
    expect(mentionQuery("ping @Ada Lovelace done", 9)).toEqual({ start: 5, query: "Ada" });
  });
  it("is nothing without an at sign, inside an address, or across a line", () => {
    expect(mentionQuery("plain words", 11)).toBeNull();
    expect(mentionQuery("mail ada@armature", 15)).toBeNull();
    expect(mentionQuery("@ada\nnext", 9)).toBeNull();
  });
});

describe("MentionTextarea", () => {
  it("asks for the words after the at sign and offers the answer", async () => {
    const asked: string[] = [];
    const findPeople = async (query: string) => {
      asked.push(query);
      return people.filter((p) => p.name.toLowerCase().includes(query.toLowerCase()));
    };
    function Harness() {
      const [text, setText] = useState("");
      return <MentionTextarea aria-label="Comment" value={text} onValueChange={setText} findPeople={findPeople} />;
    }
    render(<Harness />);
    await userEvent.type(screen.getByLabelText("Comment"), "ping @ad");
    await waitFor(() => expect(screen.getAllByRole("option").map((o) => o.getAttribute("data-mention-option"))).toEqual(["Ada Lovelace", "Adam Smith"]));
    expect(asked).toContain("ad");
    await userEvent.keyboard("{Enter}");
    expect(screen.getByLabelText("Comment")).toHaveValue("ping @Ada Lovelace ");
  });
});
