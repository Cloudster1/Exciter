/** A run of words of a comparison: the same in both, only in the newer, or only in the older. */
export interface WordRun {
  change: "same" | "added" | "removed";
  text: string;
}

const words = (text: string) => text.split(/(\s+)/).filter((each) => each !== "");

/** Two plain texts compared word by word, by their longest common run, the older's words first where both change. */
export function wordDiff(before: string, after: string): WordRun[] {
  const a = words(before);
  const b = words(after);
  // lengths[i][j] is how many words a[i..] and b[j..] share in order.
  const lengths = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lengths[i]![j] = a[i] === b[j] ? lengths[i + 1]![j + 1]! + 1 : Math.max(lengths[i + 1]![j]!, lengths[i]![j + 1]!);
    }
  }
  const out: WordRun[] = [];
  const push = (change: WordRun["change"], text: string) => {
    const last = out.at(-1);
    if (last && last.change === change) last.text += text;
    else out.push({ change, text });
  };
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      push("same", a[i]!);
      i++;
      j++;
    } else if (lengths[i + 1]![j]! >= lengths[i]![j + 1]!) {
      push("removed", a[i++]!);
    } else {
      push("added", b[j++]!);
    }
  }
  while (i < a.length) push("removed", a[i++]!);
  while (j < b.length) push("added", b[j++]!);
  return out;
}
