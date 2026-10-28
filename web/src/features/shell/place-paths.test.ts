import { describe, expect, it } from "vitest";
import type { Me } from "@/api/auth";
import { PLACE_PATHS } from "@/config";
import { placesFor } from "./places";

// The browser suite checks every place with axe by PLACE_PATHS, which cannot import places.ts (#334).
describe("the place paths the accessibility check opens", () => {
  it("are every place an administrator has, in the same order", () => {
    const admin = { role: "admin" } as Me;
    expect(placesFor(admin).map((place) => place.path)).toEqual([...PLACE_PATHS]);
  });
});
