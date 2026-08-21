// Route for /plans/:slug/* — thin wrapper, all behavior lives in
// components/plans/SlugView. Mirrors pages/Plans.tsx.
import { SlugView } from "../components/plans/SlugView";

export function PlansSlug() {
  return <SlugView />;
}
