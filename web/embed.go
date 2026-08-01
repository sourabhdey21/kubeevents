package web

import "embed"

// Static holds the embedded web UI assets.
//
//go:embed static/*
var Static embed.FS
