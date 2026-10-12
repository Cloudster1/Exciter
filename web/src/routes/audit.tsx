import { createRoute } from "@tanstack/react-router";
import { AuditLog } from "@/features/audit/AuditLog";
import { AdminsOnly } from "@/features/auth/AdminsOnly";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Who did what to the organization, newest first, for its administrators. */
export const auditRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/audit",
  component: () => (
    <AdminsOnly title={t.audit.title} refusal={t.audit.notAdmin}>
      <AuditLog />
    </AdminsOnly>
  ),
});
