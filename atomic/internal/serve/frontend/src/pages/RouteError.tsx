// RouteError — what an unknown URL or a thrown render error looks like.
//
// Without these the router falls back to its own developer-facing screen,
// which replaces the entire document: no shell, no nav, no way back except
// the browser's Back button. A typo in the address bar should be a page, not
// the end of the session.
import { Link, isRouteErrorResponse, useLocation, useRouteError } from "react-router";
import "./RouteError.css";

export function NotFoundRoute() {
  const location = useLocation();

  return (
    <div className="page-content-inner route-error">
      <p className="route-error-code">404</p>
      <h1>No such page</h1>
      <p>
        Nothing is served at <code>{location.pathname}</code>.
      </p>
      <p className="route-error-actions">
        <Link to="/">Go to the landing page</Link>
        <span className="route-error-sep">·</span>
        <Link to="/graph">Open the graph</Link>
      </p>
    </div>
  );
}

export function RouteErrorBoundary() {
  const error = useRouteError();

  // A 404 thrown by a loader is the same situation as an unmatched path;
  // anything else is a real failure and says so rather than pretending.
  if (isRouteErrorResponse(error) && error.status === 404) {
    return <NotFoundRoute />;
  }

  const detail =
    isRouteErrorResponse(error) ? `${error.status} ${error.statusText}`
    : error instanceof Error ? error.message
    : "Unknown error";

  return (
    <div className="page-content-inner route-error">
      <p className="route-error-code">error</p>
      <h1>This view failed to render</h1>
      <p>
        <code>{detail}</code>
      </p>
      <p className="route-error-actions">
        <Link to="/">Go to the landing page</Link>
      </p>
    </div>
  );
}
