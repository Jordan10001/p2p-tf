package p2ptransfer

import "embed"

// WebFS holds our embedded web templates and static assets.
//
//go:embed web/templates/* web/static/css/* web/static/js/* web/static/icons/*
var WebFS embed.FS
