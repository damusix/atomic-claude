// PageModal — the static scrim/modal markup utils/graphUI's openPageModal/
// closePageModal/wireDismiss imperatively fill and toggle (ids/classes
// ported verbatim from templates/layout.html so app.css's #cy-page-modal-scrim
// / .cy-modal* selectors and animation still apply unchanged). Mounted once
// by Shell; graphUI owns all of its content and visibility.
export function PageModal() {
  return (
    <div id="cy-page-modal-scrim">
      <div className="cy-modal" role="dialog" aria-modal="true" aria-labelledby="cy-modal-title">
        <button className="cy-modal-close" id="cy-modal-close-btn" aria-label="Close">
          <svg
            width="14"
            height="14"
            viewBox="0 0 14 14"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
          >
            <line x1="1" y1="1" x2="13" y2="13" />
            <line x1="13" y1="1" x2="1" y2="13" />
          </svg>
        </button>
        <div className="cy-modal-header">
          <span className="cy-modal-chip" id="cy-modal-chip">
            page
          </span>
          <div className="cy-modal-title" id="cy-modal-title" />
          <div className="cy-modal-desc" id="cy-modal-desc" style={{ display: "none" }} />
        </div>
        <div className="cy-modal-body" id="cy-modal-body" />
        <div className="cy-modal-actions">
          <button className="cy-modal-btn-primary" id="cy-modal-open-btn">
            Open full page
            <svg
              width="14"
              height="14"
              viewBox="0 0 14 14"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M2 7h10" />
              <polyline points="8,3 12,7 8,11" />
            </svg>
          </button>
          <button className="cy-modal-btn-ghost" id="cy-modal-dismiss-btn">
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
