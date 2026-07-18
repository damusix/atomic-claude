// FileIcon — extension-routed Font Awesome file glyph. Renders the icon's
// path data in a plain <svg> (currentColor fill, so both themes work) from
// the tree-shakeable @fortawesome icon-data packages — no icon font, no
// react-fontawesome wrapper dep.
import { faMarkdown } from "@fortawesome/free-brands-svg-icons";
import {
  faFileLines,
  faFileCode,
  faFileImage,
  faFileCsv,
  faFilePdf,
  faFolder,
  faArrowUpRightFromSquare,
  type IconDefinition,
} from "@fortawesome/free-solid-svg-icons";

const CODE_EXTS = new Set([
  "ts",
  "tsx",
  "js",
  "jsx",
  "mjs",
  "cjs",
  "go",
  "sql",
  "py",
  "rb",
  "rs",
  "sh",
  "css",
  "html",
  "json",
  "yaml",
  "yml",
  "toml",
]);
const IMAGE_EXTS = new Set(["png", "jpg", "jpeg", "gif", "svg", "webp", "ico"]);

function iconFor(relpath: string): IconDefinition {
  if (relpath.endsWith("/")) return faFolder;
  const ext = relpath.slice(relpath.lastIndexOf(".") + 1).toLowerCase();
  if (ext === "md" || ext === "markdown") return faMarkdown;
  if (CODE_EXTS.has(ext)) return faFileCode;
  if (IMAGE_EXTS.has(ext)) return faFileImage;
  if (ext === "csv") return faFileCsv;
  if (ext === "pdf") return faFilePdf;
  return faFileLines;
}

export function FaGlyph({
  icon,
  size = 13,
  className,
}: {
  icon: IconDefinition;
  size?: number;
  className?: string;
}) {
  const [w, h, , , path] = icon.icon;
  const d = Array.isArray(path) ? path.join(" ") : path;
  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox={`0 0 ${w} ${h}`}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d={d} />
    </svg>
  );
}

export function FileIcon({ relpath, className }: { relpath: string; className?: string }) {
  return <FaGlyph icon={iconFor(relpath)} className={className} />;
}

export { faFolder, faArrowUpRightFromSquare };
