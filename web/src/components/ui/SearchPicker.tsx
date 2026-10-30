import type { ReactNode } from "react";
import { Input, SelectInput } from "./controls";
import { cx } from "./cx";
import type { ControlSize } from "./Button";
import { t } from "@/i18n";

export interface PickerOption {
  value: string;
  label: string;
}

interface SearchPickerProps {
  /** Names the select, as "Workflow for Bug"; the search box is named after it. */
  "aria-label": string;
  id?: string;
  value: string;
  /** Called with the chosen value and what it is called; a leading choice has no name. */
  onChange: (value: string, label: string) => void;
  /** What is typed in the search, which the caller asks the server about. */
  words: string;
  onWords: (words: string) => void;
  /** The matches, with the chosen one among them even when the search moved past it. */
  options: PickerOption[];
  /** Choices offered before the matches, such as one that means none. */
  leading?: ReactNode;
  placeholder: string;
  /** Said under the select when the search found nothing. */
  nothing?: string;
  disabled?: boolean;
  controlSize?: ControlSize;
  className?: string;
}

/** A select over the matches of a search, so a long list is never drawn whole. */
export function SearchPicker({
  "aria-label": label,
  id,
  value,
  onChange,
  words,
  onWords,
  options,
  leading,
  placeholder,
  nothing,
  disabled,
  controlSize = "sm",
  className,
}: SearchPickerProps) {
  return (
    <div className={cx("flex flex-wrap items-center gap-1.5", className)} data-search-picker={label}>
      <Input
        type="search"
        controlSize={controlSize}
        aria-label={t.kit.find(label)}
        placeholder={placeholder}
        value={words}
        disabled={disabled}
        onChange={(event) => onWords(event.target.value)}
        // Enter in the search narrows the list; it must not submit a form around it.
        onKeyDown={(event) => event.key === "Enter" && event.preventDefault()}
        className="w-40"
        data-picker-search=""
      />
      <SelectInput
        id={id}
        aria-label={label}
        controlSize={controlSize}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value, options.find((option) => option.value === event.target.value)?.label ?? "")}
      >
        {leading}
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </SelectInput>
      {nothing && (
        <span className="text-xs text-ink-subtle" role="status">
          {nothing}
        </span>
      )}
    </div>
  );
}
