import { createRoute } from "@tanstack/react-router";
import { HomeScreen } from "@/features/home/HomeScreen";
import { appRoute } from "./app";

export const homeRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  component: HomeScreen,
});
