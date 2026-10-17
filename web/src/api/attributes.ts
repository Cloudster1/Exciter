import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { projectQueryKey } from "./projects";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A kind of requirement a project tells apart, with its icon and colour. */
export type RequirementType = Wire["RequirementType"];
export type TypeChanges = Wire["RequirementTypeInput"];
/** Something a project records about its requirements, of one kind. */
export type Attribute = Wire["Attribute"];
export type AttributeChanges = Wire["AttributeInput"];
export type AttributeKind = Wire["AttributeKind"];
export type Kind = Attribute["kind"];
export type Option = Wire["AttributeOption"];
export type OptionChange = Wire["AttributeOptionInput"];

function typesKey(projectKey: string) {
  return ["types", projectKey.toUpperCase()] as const;
}

function attributesKey(projectKey: string) {
  return ["attributes", projectKey.toUpperCase()] as const;
}

/** A project's types in order, retired ones included. */
export function useTypes(projectKey: string) {
  return useQuery({
    queryKey: typesKey(projectKey),
    queryFn: async (): Promise<RequirementType[]> => (await api.GET("/projects/{projectKey}/types", { params: { path: { projectKey } } })).data!.types,
    enabled: Boolean(projectKey),
  });
}

/** A project's attributes in order, retired ones included. */
export function useAttributes(projectKey: string) {
  return useQuery({
    queryKey: attributesKey(projectKey),
    queryFn: async (): Promise<Attribute[]> => (await api.GET("/projects/{projectKey}/attributes", { params: { path: { projectKey } } })).data!.attributes,
    enabled: Boolean(projectKey),
  });
}

/** What an attribute can be, for the form that defines one. */
export function useAttributeKinds() {
  return useQuery({
    queryKey: ["attribute-kinds"],
    queryFn: async (): Promise<AttributeKind[]> => (await api.GET("/attribute-kinds")).data!.kinds,
    staleTime: Infinity,
  });
}

/** Makes, changes and deletes a project's types; the project follows, since its default type is among them. */
export function useTypeActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () =>
    Promise.all([queryClient.invalidateQueries({ queryKey: typesKey(projectKey) }), queryClient.invalidateQueries({ queryKey: projectQueryKey(projectKey) })]);
  const path = { projectKey };
  return {
    create: useMutation({
      mutationFn: async (body: TypeChanges): Promise<RequirementType> =>
        (await api.POST("/projects/{projectKey}/types", { params: { path }, body })).data!.type,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: TypeChanges & { id: string }): Promise<RequirementType> =>
        (await api.PATCH("/projects/{projectKey}/types/{typeID}", { params: { path: { ...path, typeID: id } }, body })).data!.type,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/projects/{projectKey}/types/{typeID}", { params: { path: { ...path, typeID: id } } });
      },
      onSuccess: refresh,
    }),
  };
}

/** Defines, changes and deletes a project's attributes. */
export function useAttributeActions(projectKey: string) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: attributesKey(projectKey) });
  const path = { projectKey };
  return {
    create: useMutation({
      mutationFn: async (body: AttributeChanges): Promise<Attribute> =>
        (await api.POST("/projects/{projectKey}/attributes", { params: { path }, body })).data!.attribute,
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: async ({ id, ...body }: AttributeChanges & { id: string }): Promise<Attribute> =>
        (await api.PATCH("/projects/{projectKey}/attributes/{attributeID}", { params: { path: { ...path, attributeID: id } }, body })).data!.attribute,
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: async (id: string) => {
        await api.DELETE("/projects/{projectKey}/attributes/{attributeID}", { params: { path: { ...path, attributeID: id } } });
      },
      onSuccess: refresh,
    }),
  };
}

/** Whether an attribute shows on requirements of a type; one naming no types shows on all of them. */
export function showsOn(attribute: Attribute, typeId: string | null | undefined): boolean {
  return attribute.typeIds.length === 0 || (typeId != null && attribute.typeIds.includes(typeId));
}

// Field rules (#206): when a field is required, which choices another allows, and who changes it.

export type FieldRule = Wire["FieldRule"];
export type FieldPermit = Wire["FieldPermit"];
export type FixedField = FieldRule["field"];

const fieldRulesKey = (projectKey: string) => ["projects", "one", projectKey.toUpperCase(), "field-rules"] as const;

/** The rules of a project's fixed fields: title, statement, rationale, type, owner and priority. */
export function useFieldRules(projectKey: string) {
  return useQuery({
    queryKey: fieldRulesKey(projectKey),
    queryFn: async (): Promise<FieldRule[]> => (await api.GET("/projects/{projectKey}/field-rules", { params: { path: { projectKey } } })).data!.rules,
  });
}

/** Sets one fixed field's rule whole. */
export function useSaveFieldRule(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ field, ...body }: Wire["FieldRuleInput"] & { field: FixedField }): Promise<FieldRule> =>
      (await api.PUT("/projects/{projectKey}/field-rules/{field}", { params: { path: { projectKey, field } }, body })).data!.rule,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: fieldRulesKey(projectKey) }),
  });
}

/** The options a dependent select may hold while the attribute it depends on holds what values say; all of them
 * where that one's option names none. */
export function allowedOptions(attribute: Attribute, values: Record<string, unknown>): Attribute["options"] {
  const dependency = attribute.dependsOn;
  const parent = dependency ? values[dependency.attributeId] : undefined;
  const allowed = dependency && typeof parent === "string" ? dependency.choices[parent] : undefined;
  return allowed ? attribute.options.filter((option) => allowed.includes(option.id)) : attribute.options;
}

/** The roles that may change a field under its permits while in a status: null when the reader may; otherwise who
 * may, empty when nobody does. */
export function permittedRoles(permits: FieldPermit[], statusId: string | null | undefined, roles: string[]): string[] | null {
  if (permits.length === 0) return null;
  const who: string[] = [];
  for (const permit of permits) {
    if (permit.statuses.length > 0 && (!statusId || !permit.statuses.includes(statusId))) continue;
    if (permit.roles.some((role) => roles.includes(role))) return null;
    for (const role of permit.roles) if (!who.includes(role)) who.push(role);
  }
  return who;
}
