package web

import _ "embed"

//go:embed console.html
var ConsoleHTML []byte

//go:embed debug.html
var DebugHTML []byte
