// Package mlinzi embeds the civic dataset into the compiled binary: the
// situation guides in data/ and the Resources Center's legal documents in
// resources/. They are two folders, loaded by two stores, because they are
// two different things — see internal/resource's package comment.
//
// Embedding both matters for the brief's low-bandwidth / basic-device
// constraint: a person on a shared or low-end device should be able to run
// one file with no installation step, no external data directory to keep
// alongside it, and no network access required to look up their rights, the
// next steps for their situation, or the text of a provision they have been
// told about.
package mlinzi

import "embed"

//go:embed all:data
var DataFS embed.FS

//go:embed all:resources
var ResourceFS embed.FS
