package embedded

import _ "embed"

// Each pattern must match exactly one file for a string embed to compile.
// The CLI regression test adds private env files that must be filtered out.
//
//go:embed .env*
var RootExample string

//go:embed config/.env*
var NestedExample string
