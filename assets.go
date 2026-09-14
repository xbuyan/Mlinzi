// Package mlinzi embeds the civic guide dataset into the compiled binary.
//
// This matters for the brief's low-bandwidth / basic-device constraint: a
// person on a shared or low-end device should be able to run one file with
// no installation step, no external data directory to keep alongside it, and
// no network access required to look up their rights and next steps.
package mlinzi

import "embed"

//go:embed all:data
var DataFS embed.FS
