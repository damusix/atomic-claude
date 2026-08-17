package claudeinstall

// InstallWithOutput exposes installWithOutput to external test files without
// putting it on the public API.
var InstallWithOutput = installWithOutput

// PatchAgentContent exposes patchAgentContent for unit testing.
var PatchAgentContent = patchAgentContent
