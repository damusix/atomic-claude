// Barrel for generic, app-agnostic UI primitives. Consumers import from this
// barrel only — app-specific components (nav/, rail/, ...) are never barreled
// and are imported directly. See frontend/CLAUDE.md.
export { FileIcon, FaGlyph, faFolder, faArrowUpRightFromSquare } from "./FileIcon/FileIcon";
export { Tooltip } from "./Tooltip/Tooltip";
