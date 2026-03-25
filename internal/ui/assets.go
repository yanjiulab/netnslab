package ui

import "embed"

// staticFS contains the bundled web UI assets.
//
// NOTE: Keeping UI files under internal/ui/static/* allows embedding them
// directly into the netnslab binary.
//
// To update the UI during development, change the files under
// internal/ui/static and rebuild.
//
// For a pure "dev hot reload" mode, we can later add an env override to
// serve from disk.
//go:embed static/*
var staticFS embed.FS

