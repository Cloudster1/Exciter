import { useEffect, useRef, useState } from "react";
import { MIN_DATE_YEAR } from "@/config";
import { Input, type InputProps } from "./controls";

const ISO_DAY = /^(\d{4,})-(\d{2})-(\d{2})$/;

/** A whole date someone could mean: typing a year by hand passes through 0002 and 0020 first. */
export function isCompleteDate(value: string): boolean {
  const match = ISO_DAY.exec(value);
  return match !== null && Number(match[1]) >= MIN_DATE_YEAR && !Number.isNaN(Date.parse(value));
}

/** A date that saves itself on a complete date or on leaving the input, so a
 * year on its way from 0002 to 2026 never reaches the server. */
export function DateInput({
  value,
  onCommit,
  ...rest
}: Omit<InputProps, "type" | "value" | "onChange" | "onBlur"> & {
  /** The saved day as YYYY-MM-DD, or empty for none. */
  value: string;
  /** Called with the day to save, or an empty string to clear it. */
  onCommit: (value: string) => void;
}) {
  // What is typed lives here until it is saved, so a half-typed year is not
  // snapped back to the saved one by the next render.
  const [draft, setDraft] = useState(value);
  const sent = useRef(value);
  useEffect(() => {
    setDraft(value);
    sent.current = value;
  }, [value]);

  function commit(next: string) {
    if (next === sent.current) return;
    sent.current = next;
    onCommit(next);
  }

  return (
    <Input
      {...rest}
      type="date"
      value={draft}
      onChange={(event) => {
        const next = event.target.value;
        setDraft(next);
        if (isCompleteDate(next)) commit(next);
      }}
      onBlur={(event) => {
        // A browser reports a half-filled date as empty and flags it badInput;
        // leaving one of those is not asking to clear the date.
        if (event.target.validity?.badInput) return;
        if (draft === "" || isCompleteDate(draft)) commit(draft);
      }}
    />
  );
}
