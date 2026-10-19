/** The first refusal of a rating among a form's field errors: the rating as a whole, or one of its values. */
export function riskError(fields: Record<string, string>): string | undefined {
  return Object.entries(fields).find(([field]) => field === "risk" || field.startsWith("risk."))?.[1];
}
