import { useArmatureAccount } from "@/api/armature";
import { useStatorAccount } from "@/api/stator";
import { ARMATURE_ICON_PATH, STATOR_ICON_PATH } from "@/config";
import { t } from "@/i18n";

/** Another tool of the installation, opened at its home under the address its connection was set up with. */
export interface ConnectedTool {
  id: "armature" | "stator";
  name: string;
  href: string;
  icon: string;
}

/** The tools Exciter is connected to. The installation's connection decides, not the person's own account there. */
export function useConnectedTools(): ConnectedTool[] {
  const { data: armature } = useArmatureAccount();
  const { data: stator } = useStatorAccount();
  const tools: ConnectedTool[] = [];
  if (armature?.configured && armature.baseUrl) {
    tools.push({ id: "armature", name: t.nav.armature, href: homeOf(armature.baseUrl), icon: ARMATURE_ICON_PATH });
  }
  if (stator?.configured && stator.baseUrl) {
    tools.push({ id: "stator", name: t.nav.stator, href: homeOf(stator.baseUrl), icon: STATOR_ICON_PATH });
  }
  return tools;
}

function homeOf(baseUrl: string): string {
  return `${baseUrl.replace(/\/+$/, "")}/`;
}
