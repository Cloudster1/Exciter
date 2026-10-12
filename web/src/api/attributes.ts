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
