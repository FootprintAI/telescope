// Package version holds the telescope build version. It is a dependency-free
// leaf package so any component (CLI, report, API clients) can stamp the
// version without creating a backwards dependency on the command layer.
package version

// V is overridden at build time via
// -ldflags "-X github.com/footprintai/telescope/internal/version.V=vX.Y.Z".
var V = "dev"
