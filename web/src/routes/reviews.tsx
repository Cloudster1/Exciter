import { createRoute } from "@tanstack/react-router";
import { REVIEWS_PATH } from "@/config";
import { WaitingReviewsScreen } from "@/features/reviews/WaitingReviews";
import { appRoute } from "./app";

/** The reviews waiting for the reader's decision. */
export const reviewsRoute = createRoute({ getParentRoute: () => appRoute, path: REVIEWS_PATH, component: WaitingReviewsScreen });
