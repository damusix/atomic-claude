// Contents — the on-page heading tree (h1-h4), built from what pages/Page
// publishes after injecting the server HTML. Anchors come from the ids
// goldmark already assigned, so every entry here is guaranteed to land.
import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { events, getPageHeadings, type PageHeading } from "../../utils/events";

export function Contents() {
  // Seeded from the latch: the page has usually already rendered and
  // emitted by the time this mounts (see utils/events).
  const [headings, setHeadings] = useState<PageHeading[]>(getPageHeadings);
  const navigate = useNavigate();
  const location = useLocation();

  useEffect(() => {
    return events.on("page.headings", ({ headings: next }) => setHeadings(next));
  }, []);

  if (!headings.length) {
    return <span className="rail-empty">no headings</span>;
  }

  // The shallowest level present becomes the left edge, so a document whose
  // top level is h2 does not render every entry indented by one step.
  const minLevel = Math.min(...headings.map((h) => h.level));

  return (
    <ul className="rail-toc">
      {headings.map((heading) => (
        <li key={heading.id} style={{ paddingLeft: `${(heading.level - minLevel) * 11}px` }}>
          <a
            className="rail-toc-link"
            href={`#${heading.id}`}
            data-level={heading.level}
            onClick={(e) => {
              // Routed rather than left to the browser: the scroll container
              // is #main-pane, and useHashScroll is what knows how to move it.
              e.preventDefault();
              navigate(`${location.pathname}#${heading.id}`);
            }}
          >
            {heading.text}
          </a>
        </li>
      ))}
    </ul>
  );
}
