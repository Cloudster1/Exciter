import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Pager, clampOffset, countOf, shownOf } from "./Pager";

const pageSize = 50;
const total = 250;

describe("Pager", () => {
  it("says which rows of the whole list are shown and goes on to the next", () => {
    const onChange = vi.fn();
    render(<Pager label="Queue pages" total={total} offset={0} pageSize={pageSize} onChange={onChange} />);
    expect(screen.getByText("1 to 50 of 250")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(onChange).toHaveBeenCalledWith(50);
  });

  it("stops at the last page and goes back from it", () => {
    const onChange = vi.fn();
    render(<Pager label="Queue pages" total={total} offset={200} pageSize={pageSize} onChange={onChange} />);
    expect(screen.getByText("201 to 250 of 250")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
    expect(onChange).toHaveBeenCalledWith(150);
  });

  it("counts a short last page by the rows it holds", () => {
    render(<Pager label="Issue pages" total={205} offset={200} pageSize={100} onChange={() => {}} />);
    expect(screen.getByText("201 to 205 of 205")).toBeInTheDocument();
  });

  it("is named for the list it pages", () => {
    render(<Pager label="Issue pages" total={total} offset={0} pageSize={pageSize} onChange={() => {}} />);
    expect(screen.getByRole("navigation", { name: "Issue pages" })).toBeInTheDocument();
  });

  it("is not drawn for a list that fits on one page", () => {
    const { container } = render(<Pager label="Queue pages" total={pageSize} offset={0} pageSize={pageSize} onChange={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });
});

const request = { one: "request", many: "requests" };
const issue = { one: "issue", many: "issues" };

describe("the count", () => {
  it("is the whole list, not the page", () => {
    expect(countOf(total, request)).toBe("250 requests");
    expect(countOf(1, request)).toBe("1 request");
    expect(countOf(0, request)).toBe("0 requests");
  });

  it("says which of the whole list are shown once it runs past a page", () => {
    expect(shownOf(450, 100, 100, issue)).toBe("101 to 200 of 450 issues");
    expect(shownOf(205, 200, 5, issue)).toBe("201 to 205 of 205 issues");
    expect(shownOf(42, 0, 42, issue)).toBe("42 issues");
    expect(shownOf(1, 0, 1, issue)).toBe("1 issue");
  });

  it("brings a page that ran past a shrunken list back to its last page", () => {
    expect(clampOffset(200, 120, pageSize)).toBe(100);
    expect(clampOffset(100, 120, pageSize)).toBe(100);
    expect(clampOffset(50, 0, pageSize)).toBe(0);
  });
});

describe("the count in German", () => {
  it("takes the plural the catalogue gives, not an English s", async () => {
    // Read through the module: t is a live binding, and a copy would keep English.
    const i18n = await import("@/i18n");
    i18n.applyLanguage("de");
    try {
      expect(countOf(3, i18n.t.kit.counted.requirement)).toBe("3 Anforderungen");
      expect(countOf(1, i18n.t.kit.counted.requirement)).toBe("1 Anforderung");
      expect(shownOf(450, 100, 100, i18n.t.kit.counted.requirement)).toBe("101 bis 200 von 450 Anforderungen");
    } finally {
      i18n.applyLanguage("");
    }
  });
});
