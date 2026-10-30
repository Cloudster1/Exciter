import { useEffect, useRef, useState } from "react";
import { Input, type InputProps } from "./controls";
import { LIST_SEARCH_DELAY_MS } from "@/config";

type ListSearchProps = Omit<InputProps, "value" | "onChange" | "type"> & {
  /** What the list is searched for now, as the address holds it. */
  value: string;
  /** Called with the trimmed text once typing pauses, not per letter. */
  onChange: (text: string) => void;
  delayMs?: number;
};

/** The search box over a paged list; it hands on what was typed once typing pauses. */
export function ListSearch({ value, onChange, delayMs = LIST_SEARCH_DELAY_MS, ...rest }: ListSearchProps) {
  const [text, setText] = useState(value);
  const timer = useRef<number | undefined>(undefined);
  const latest = useRef(onChange);
  useEffect(() => {
    latest.current = onChange;
  });
  // A change from elsewhere, such as going back, shows in the box; while typing, the box leads.
  useEffect(() => {
    if (timer.current === undefined) setText(value);
  }, [value]);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  function type(next: string) {
    setText(next);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      timer.current = undefined;
      latest.current(next.trim());
    }, delayMs);
  }

  return <Input type="search" controlSize="sm" {...rest} value={text} onChange={(event) => type(event.target.value)} />;
}
