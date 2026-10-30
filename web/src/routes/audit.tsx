import { createRoute } from "@tanstack/react-router";
import { AUDIT_PATH } from "@/config";
import { AuditLog } from "@/features/audit/AuditLog";
import { adminRoute } from "./admin";

/** Who did what in Exciter, newest first, for the administrators. */
export const auditRoute = createRoute({ getParentRoute: () => adminRoute, path: AUDIT_PATH, component: AuditLog });
