package version

import "fmt"

var (
	Version   = "0.1.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func PrintVersion() {
	fmt.Printf("chawrtd version: %s\n", Version)
	fmt.Printf("git commit:      %s\n", GitCommit)
	fmt.Printf("build time:      %s\n", BuildTime)
}
