// Route for /plans — thin wrapper, all behavior lives in components/plans/PlansView.
import { PlansView } from "../components/plans/PlansView";

export function Plans() {
  return <PlansView />;
}
