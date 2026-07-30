// App — entry: providers (fetch, events) + router + Shell mount.
import { createBrowserRouter, RouterProvider } from "react-router";
import { ApiProvider } from "./utils/api";
import { EventsProvider } from "./utils/events";
import { routes } from "./routes";

const router = createBrowserRouter(routes);

export function App() {
  return (
    <EventsProvider>
      <ApiProvider>
        <RouterProvider router={router} />
      </ApiProvider>
    </EventsProvider>
  );
}
