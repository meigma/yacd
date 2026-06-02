// Package exec is a small command-runner seam for the YACD CLI.
//
// Runner executes an external command and captures stdout and stderr
// separately. The repository otherwise reaches for os/exec directly, which
// leaves command-shelling adapters untestable without the real binary; this
// seam lets adapters such as cluster/k3d depend on an interface and substitute
// a fake in unit tests, while OS returns the real os/exec-backed implementation.
package exec
