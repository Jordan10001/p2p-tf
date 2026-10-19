package p2ptransfer

import "embed"

// WebFS holds our embedded web templates and static assets.
//
//go:embed web/static/icons/* web/static/dist/*
var WebFS embed.FS
