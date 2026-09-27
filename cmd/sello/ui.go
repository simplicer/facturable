// UI HTML for the stamp-first experience. Kept in its own file to avoid
// bloating main.go. Served at GET / via go:embed.
package main

import _ "embed"

//go:embed ui.html
var formUI string
