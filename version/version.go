package version

// Version components
const (
	Maj = "0"
	Min = "0"
	Fix = "1"
)

var (
	// Version is the current version of Spacemesh
	Version = "0.19.5"
	// Branch is the current branch of Spacemesh
	Branch = ""
	// GitCommit is the current HEAD set using ldflags.
	GitCommit string
)

func init() {
	if GitCommit != "" {
		Version += "-" + GitCommit
	}
}
