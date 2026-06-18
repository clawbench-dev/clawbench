package pi

// PiACPInputRemaps maps Pi ACP input field names to canonical names.
// Pi uses the standard ACP field names, so only the CLI-specific remap
// ("path" -> "file_path") is needed. The generic 6-field normalization
// is applied automatically by the ACP framework when InputRemaps is empty.
var PiACPInputRemaps = map[string]string{}
