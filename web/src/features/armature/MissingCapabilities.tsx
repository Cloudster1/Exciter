import type { ArmatureAccount } from "@/api/armature";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** What the connected Armature does not offer that Exciter needs, each sentence saying what goes and what to do. */
export function MissingCapabilities({ missing }: { missing: ArmatureAccount["missing"] }) {
  if (missing.length === 0) return null;
  return (
    <div role="note" aria-label={t.armature.missingTitle} className="space-y-1 text-sm text-ink" data-armature-missing>
      <p className="font-medium">{t.armature.missingTitle}</p>
      <ul className="space-y-1">
        {missing.map((each) => (
          <li key={each.capability} className="flex items-start gap-1.5" data-armature-missing-capability={each.capability}>
            <Icon.Warning aria-hidden="true" className="mt-0.5 shrink-0" />
            {each.message}
          </li>
        ))}
      </ul>
    </div>
  );
}
