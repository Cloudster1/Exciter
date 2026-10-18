import type { InputHTMLAttributes, ReactNode, Ref, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";
import { controlHeight, type ControlSize } from "./Button";
import { cx } from "./cx";

/** The look every text input and select shares. */
export const controlClass =
  "rounded-control border border-border bg-surface px-2.5 text-ink placeholder:text-ink-subtle hover:border-border-strong focus:border-accent focus:outline-none disabled:bg-surface-raised disabled:text-ink-disabled";

/** The control look at one of the three heights. */
export function control(size: ControlSize = "md", ...extra: Array<string | false | null | undefined>): string {
  return cx(controlClass, controlHeight[size], ...extra);
}

// The native size attribute means "characters wide"; the kit sizes by height through controlSize.
// The ref is a plain prop, so a caller can focus the input it just rendered.
export type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, "size"> & { controlSize?: ControlSize; invalid?: boolean; ref?: Ref<HTMLInputElement> };

export function Input({ className, controlSize = "md", invalid, ...rest }: InputProps) {
  return <input {...rest} aria-invalid={invalid || undefined} className={control(controlSize, "block w-full", invalid && "border-danger", className)} />;
}

export function Textarea({ className, invalid, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement> & { invalid?: boolean; ref?: Ref<HTMLTextAreaElement> }) {
  return (
    <textarea
      {...rest}
      aria-invalid={invalid || undefined}
      className={cx(controlClass, "block w-full py-2 text-sm leading-5", invalid && "border-danger", className)}
    />
  );
}

/** A checkbox with its words beside it, one click target. */
export function Checkbox({ label, className, ...rest }: InputHTMLAttributes<HTMLInputElement> & { label: ReactNode }) {
  return (
    <label className={cx("inline-flex items-center gap-2 text-sm text-ink", rest.disabled && "text-ink-disabled", className)}>
      <input {...rest} type="checkbox" className="size-4 rounded-[4px] border-border-strong accent-accent" />
      {label}
    </label>
  );
}

/** One of a few options sharing a name, with its words and an optional sentence under them, one click target. */
export function Radio({ label, hint, className, ...rest }: InputHTMLAttributes<HTMLInputElement> & { label: ReactNode; hint?: ReactNode }) {
  return (
    <label className={cx("flex items-start gap-2 text-sm text-ink", rest.disabled && "text-ink-disabled", className)}>
      <input {...rest} type="radio" className="mt-0.5 size-4 shrink-0 accent-accent" />
      <span>
        {label}
        {hint && <span className="block text-xs text-ink-subtle">{hint}</span>}
      </span>
    </label>
  );
}

/** An on or off that takes effect at once, which is what tells it from a checkbox. */
export function Switch({
  checked,
  onChange,
  label,
  disabled,
  className,
  ...rest
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  disabled?: boolean;
  className?: string;
} & Record<`data-${string}`, string | undefined>) {
  return (
    <button
      {...rest}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cx(
        "relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors",
        checked ? "border-accent bg-accent" : "border-border-strong bg-surface-raised",
        disabled && "cursor-not-allowed opacity-50",
        className,
      )}
    >
      <span
        aria-hidden="true"
        className={cx("size-3.5 rounded-full bg-surface-overlay shadow-1 transition-transform", checked ? "translate-x-[18px]" : "translate-x-[2px]")}
      />
    </button>
  );
}

/** The label, hint and error around a control, with the ids the control reads. */
export function Labelled({
  id,
  label,
  hint,
  error,
  children,
  className,
}: {
  id: string;
  label: ReactNode;
  hint?: string;
  error?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cx("space-y-1", className)}>
      <label htmlFor={id} className="block text-sm font-medium text-ink-muted">
        {label}
      </label>
      {children}
      {error ? (
        <p id={`${id}-error`} className="text-sm text-danger">
          {error}
        </p>
      ) : hint ? (
        <p id={`${id}-hint`} className="text-sm text-ink-subtle">
          {hint}
        </p>
      ) : null}
    </div>
  );
}

/** The id a labelled control gets from its label, which the browser tests read. */
export function fieldId(label: string): string {
  return `field-${label.toLowerCase().replace(/\s+/g, "-")}`;
}

export function describedBy(id: string, hint?: string, error?: string): string | undefined {
  return error ? `${id}-error` : hint ? `${id}-hint` : undefined;
}

/** A select on its own, named by aria-label, for a toolbar or a table cell. */
export function SelectInput({
  className,
  controlSize = "md",
  invalid,
  children,
  ...rest
}: SelectHTMLAttributes<HTMLSelectElement> & { controlSize?: ControlSize; invalid?: boolean; ref?: Ref<HTMLSelectElement> }) {
  return (
    <select {...rest} aria-invalid={invalid || undefined} className={control(controlSize, "block", invalid && "border-danger", className)}>
      {children}
    </select>
  );
}

/** A select drawn like a Field, with the same label treatment. */
export function Select({
  label,
  id,
  className,
  children,
  hint,
  error,
  controlSize = "md",
  ...rest
}: SelectHTMLAttributes<HTMLSelectElement> & { label: string; hint?: string; error?: string; controlSize?: ControlSize }) {
  const selectId = id ?? fieldId(label);
  return (
    <Labelled id={selectId} label={label} hint={hint} error={error}>
      <SelectInput
        {...rest}
        id={selectId}
        invalid={Boolean(error)}
        aria-describedby={describedBy(selectId, hint, error)}
        controlSize={controlSize}
        className={cx("w-full", className)}
      >
        {children}
      </SelectInput>
    </Labelled>
  );
}
