import { useId, useState, type KeyboardEvent } from "react";
import { IconButton, Input, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TERM_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** A list of words, each taken with Enter or a comma and taken off with its own button; Backspace in an empty box
 * takes the last one off, and what is typed is taken when the box loses focus. */
export function WordsInput({
  label,
  hint,
  words,
  onChange,
  most,
  error,
  disabled,
  ...rest
}: {
  label: string;
  hint?: string;
  words: string[];
  onChange: (words: string[]) => void;
  most: number;
  error?: string;
  disabled?: boolean;
  [attr: `data-${string}`]: string | undefined;
}) {
  const id = useId();
  const [text, setText] = useState("");
  const full = words.length >= most;
  const problem = error ?? (full && text.trim() ? t.glossary.words.tooMany(most) : undefined);

  function take() {
    const word = text.split(/\s+/).filter(Boolean).join(" ");
    if (!word || full) return;
    if (!words.some((each) => each.toLowerCase() === word.toLowerCase())) onChange([...words, word]);
    setText("");
  }

  function keys(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter" || event.key === ",") {
      event.preventDefault();
      take();
    } else if (event.key === "Backspace" && text === "" && words.length > 0) {
      onChange(words.slice(0, -1));
    }
  }

  const described = [problem ? `${id}-error` : hint ? `${id}-hint` : ""].filter(Boolean).join(" ") || undefined;
  return (
    <div className="space-y-1" {...rest}>
      <label htmlFor={id} className="block text-sm font-medium text-ink-muted">
        {label}
      </label>
      {words.length > 0 && (
        <ul className="flex flex-wrap gap-1.5" aria-label={label}>
          {words.map((word) => (
            <li key={word}>
              <Tag className="gap-1 py-0.5 text-xs" data-word={word}>
                {word}
                {!disabled && (
                  <IconButton
                    icon={<Icon.X />}
                    label={t.glossary.words.remove(word)}
                    size="xs"
                    onClick={() => onChange(words.filter((each) => each !== word))}
                  />
                )}
              </Tag>
            </li>
          ))}
        </ul>
      )}
      <Input
        id={id}
        value={text}
        maxLength={TERM_NAME_MAX_LENGTH}
        disabled={disabled}
        invalid={Boolean(problem)}
        aria-describedby={described}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={keys}
        onBlur={take}
      />
      {problem ? (
        <p id={`${id}-error`} className="text-sm text-danger">
          {problem}
        </p>
      ) : hint ? (
        <p id={`${id}-hint`} className="text-sm text-ink-subtle">
          {hint}
        </p>
      ) : null}
    </div>
  );
}
