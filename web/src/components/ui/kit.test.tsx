import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Chip, Count, ErrorBanner, Page, Radio, Tabs, Toolbar, WordButton } from "./index";
import { COUNT_MAX } from "@/config";

describe("Chip", () => {
  it("cuts a long name short to fit, and keeps the whole of it as its title", () => {
    render(<Chip pressed={false}>A label with a name far longer than any filter row is wide</Chip>);
    const chip = screen.getByRole("button", { name: /A label with a name/ });
    expect(chip).toHaveAttribute("title", "A label with a name far longer than any filter row is wide");
    expect(chip.querySelector(".truncate")?.textContent).toBe("A label with a name far longer than any filter row is wide");
  });
});

describe("Tabs", () => {
  it("scrolls its own row on a narrow screen, so a page with many tabs never widens", () => {
    render(
      <Tabs label="Space settings" value="a" tabs={["a", "b", "c", "d", "e"].map((v) => ({ value: v, label: v }))} onChange={() => {}} className="mb-4" />,
    );
    const list = screen.getByRole("tablist", { name: "Space settings" });
    expect(list.className).toContain("overflow-x-auto");
    expect(list.parentElement!.className).toContain("mb-4");
  });
});

describe("ErrorBanner", () => {
  it("brings itself into view when asked to, and only then", () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    try {
      const { unmount } = render(<ErrorBanner>Quiet</ErrorBanner>);
      expect(scroll).not.toHaveBeenCalled();
      unmount();
      render(<ErrorBanner reveal>The name is taken.</ErrorBanner>);
      expect(scroll).toHaveBeenCalledWith({ block: "nearest" });
    } finally {
      // jsdom has none of its own.
      delete (Element.prototype as Partial<Element>).scrollIntoView;
    }
  });
});

describe("Radio", () => {
  it("is one click target with its words and its sentence", async () => {
    const onChange = vi.fn();
    render(<Radio name="mode" value="live" label="Live" hint="Changes show as they are typed." onChange={onChange} />);
    await userEvent.click(screen.getByText("Changes show as they are typed."));
    expect(screen.getByRole("radio", { name: /Live/ })).toBeChecked();
    expect(onChange).toHaveBeenCalled();
  });
});

describe("WordButton", () => {
  it("is a button that never submits the form it sits in", () => {
    render(<WordButton>Ada Lovelace</WordButton>);
    expect(screen.getByRole("button", { name: "Ada Lovelace" })).toHaveAttribute("type", "button");
  });
});

describe("Page", () => {
  it("gives a page one of the suite's three widths", () => {
    const { container, rerender } = render(<Page width="narrow">Form</Page>);
    expect(container.firstElementChild).toHaveClass("max-w-[64rem]");
    rerender(<Page>Reading</Page>);
    expect(container.firstElementChild).toHaveClass("max-w-[100rem]");
    rerender(<Page width="wide">Canvas</Page>);
    expect(container.firstElementChild).toHaveClass("w-full");
  });
});

describe("Count", () => {
  it("caps what it writes, says it in full, and shows nothing for none", () => {
    const { container, rerender } = render(<Count count={0} name="reviews" says="no reviews" />);
    expect(container).toBeEmptyDOMElement();
    rerender(<Count count={COUNT_MAX + 1} name="reviews" says={`${COUNT_MAX + 1} reviews wait`} />);
    expect(container.querySelector("[data-count=reviews]")).toHaveTextContent(`${COUNT_MAX}+, ${COUNT_MAX + 1} reviews wait`);
  });
});

describe("Toolbar", () => {
  it("is a named toolbar with what narrows on the left", () => {
    render(<Toolbar label="Filters" start={<button type="button">Mine</button>} end={<button type="button">New</button>} />);
    expect(screen.getByRole("toolbar", { name: "Filters" })).toContainElement(screen.getByRole("button", { name: "Mine" }));
  });
});
