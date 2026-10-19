import type { Dimension, Rating, RatingSide, RiskLevel, RiskScheme, RiskSchemeInput } from "@/api/risks";
import { RISK_DEFAULT_LEVELS, RISK_DEFAULT_SCALE } from "@/config";
import { t } from "@/i18n";

// What a rating gives under a scheme, worked out in the browser as the server's risk_level_of does, so the editor
// shows a level as the values are picked.

/** A side's priority number: its values multiplied, detection only when the scheme rates it; null while one is unknown. */
export function rpnOf(scheme: Pick<RiskScheme, "detection">, side: RatingSide): number | null {
  if (side.severity == null || side.probability == null) return null;
  if (!scheme.detection) return side.severity * side.probability;
  return side.detection == null ? null : side.severity * side.probability * side.detection;
}

/** The level a side gives: the matrix's cell, or the highest level whose threshold its priority number reaches. */
export function levelOf(scheme: Pick<RiskScheme, "method" | "detection" | "levels" | "matrix">, side: RatingSide): RiskLevel | null {
  if (scheme.method === "matrix") {
    const cell = scheme.matrix.find((c) => c.severity === side.severity && c.probability === side.probability);
    return scheme.levels.find((l) => l.name === cell?.level) ?? null;
  }
  const rpn = rpnOf(scheme, side);
  if (rpn == null) return null;
  return [...scheme.levels].reverse().find((l) => (l.minRpn ?? 0) <= rpn) ?? null;
}

/** A side with no value yet. */
export function emptySide(): RatingSide {
  return { severity: null, probability: null, detection: null };
}

/** A rating to edit, empty for a risk not rated yet. */
export function draftRating(rating: Rating | null | undefined): Rating {
  return { before: { ...emptySide(), ...rating?.before }, after: { ...emptySide(), ...rating?.after } };
}

/** Whether a rating gives any value. */
export function rated(rating: Rating): boolean {
  return [rating.before, rating.after].some((s) => s.severity != null || s.probability != null || s.detection != null);
}

/** The scheme a project starts from: five values a scale and three levels by the product of severity and probability. */
export function defaultScheme(): RiskSchemeInput {
  const values = (dimension: Dimension) =>
    Array.from({ length: RISK_DEFAULT_SCALE }, (_, i) => ({ value: i + 1, name: t.risks.valueName(dimension, i + 1), description: "" }));
  const matrix = [];
  for (let severity = 1; severity <= RISK_DEFAULT_SCALE; severity++) {
    for (let probability = 1; probability <= RISK_DEFAULT_SCALE; probability++) {
      const level = [...RISK_DEFAULT_LEVELS].reverse().find((l) => severity * probability >= l.from)!;
      matrix.push({ severity, probability, level: level.name });
    }
  }
  return {
    method: "matrix",
    detection: false,
    scales: (["severity", "probability"] as const).map((dimension) => ({ dimension, label: t.risks.dimensions[dimension], values: values(dimension) })),
    levels: RISK_DEFAULT_LEVELS.map(({ name, colour, acceptable }) => ({ name, colour, acceptable, minRpn: null })),
    matrix,
  };
}
