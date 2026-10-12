import type { InputHTMLAttributes, TextareaHTMLAttributes } from "react";
import { type ControlSize } from "./Button";
import { Input, Labelled, Textarea, describedBy, fieldId } from "./controls";

type FieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, "size"> & {
  label: string;
  error?: string;
  hint?: string;
  controlSize?: ControlSize;
  /** Rows make it a textarea; the label and ids stay the same. */
  rows?: number;
};

// The id is derived from the label so a form's fields are addressable by what
// a person reads; the browser tests find inputs the same way.
export function Field({ label, error, hint, id, rows, controlSize, ...rest }: FieldProps) {
  const inputId = id ?? fieldId(label);
  const shared = { id: inputId, "aria-invalid": error ? true : undefined, "aria-describedby": describedBy(inputId, hint, error) };
  return (
    <Labelled id={inputId} label={label} hint={hint} error={error}>
      {rows ? (
        <Textarea {...(rest as TextareaHTMLAttributes<HTMLTextAreaElement>)} {...shared} rows={rows} invalid={Boolean(error)} />
      ) : (
        <Input {...rest} {...shared} controlSize={controlSize} invalid={Boolean(error)} />
      )}
    </Labelled>
  );
}
