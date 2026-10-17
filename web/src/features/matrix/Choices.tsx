import { Chip } from "@/components/ui";

/** Several of a few things picked by pressing them; none picked counts every one, as the hint says. */
export function Choices({
  label,
  hint,
  name,
  options,
  chosen,
  onChange,
  quiet = false,
  groupLabel,
}: {
  label: string;
  hint: string;
  name: string;
  options: Array<{ value: string; label: string }>;
  chosen: string[];
  onChange: (next: string[]) => void;
  /** Keeps the hint back while something else already says what counts. */
  quiet?: boolean;
  /** The group's name where the shown label repeats on the page, such as one per trace level. */
  groupLabel?: string;
}) {
  const toggle = (value: string) => onChange(chosen.includes(value) ? chosen.filter((each) => each !== value) : [...chosen, value]);
  return (
    <div role="group" aria-label={groupLabel ?? label} className="min-w-64 flex-1 space-y-1">
      <span className="block text-xs font-medium text-ink-muted">{label}</span>
      <div className="flex flex-wrap gap-1.5">
        {options.map((option) => (
          <Chip key={option.value} pressed={chosen.includes(option.value)} onClick={() => toggle(option.value)} data-choice={`${name}:${option.value}`}>
            {option.label}
          </Chip>
        ))}
      </div>
      {chosen.length === 0 && !quiet && <p className="text-xs text-ink-subtle">{hint}</p>}
    </div>
  );
}
