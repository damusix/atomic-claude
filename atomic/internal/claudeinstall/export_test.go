package claudeinstall

// InstallWithOutput exposes installWithOutput for use in external test files.
// Production code uses the unexported form; this shim keeps it off the public API.
var InstallWithOutput = installWithOutput

// PatchAgentContent exposes patchAgentContent for unit testing of the
// model-tier frontmatter patching logic (CP4 — install-time agent overrides).
var PatchAgentContent = patchAgentContent
