import type { ReactNode } from "react";
import { t } from "@/i18n";
import { Input, Labelled, describedBy, fieldId } from "./controls";
import { cx } from "./cx";

// The browser's picker beside the value as text, so a colour can be pointed at or typed;
// the picker only knows six-digit hex, so anything else shows as text alone.
export function ColorField({
  label,
  id,
  value,
  onChange,
  placeholder,
  hint,
  className,
  children,
  ...rest
}: {
  label: string;
  id?: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  hint?: string;
  className?: string;
  /** Drawn after the value, for a reset or a note. */
  children?: ReactNode;
} & Record<`data-${string}`, string | undefined>) {
  const inputId = id ?? fieldId(label);
  const pickable = /^#[0-9a-fA-F]{6}$/.test(value) ? value : /^#[0-9a-fA-F]{6}$/.test(placeholder ?? "") ? (placeholder as string) : "#000000";
  return (
    <Labelled id={inputId} label={label} hint={hint} className={className}>
      <span className="flex items-center gap-2">
        <input
          type="color"
          aria-label={t.kit.pick(label)}
          value={pickable}
          onChange={(e) => onChange(e.target.value)}
          className={cx("size-8 shrink-0 cursor-pointer rounded-control border border-border-strong bg-surface p-0.5")}
        />
        <Input
          {...rest}
          id={inputId}
          value={value}
          placeholder={placeholder}
          onChange={(e) => onChange(e.target.value)}
          aria-describedby={describedBy(inputId, hint)}
          controlSize="sm"
          className="w-36 font-mono"
          spellCheck={false}
        />
        {children}
      </span>
    </Labelled>
  );
}
