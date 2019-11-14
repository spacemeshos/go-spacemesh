package integration

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	// compileMtx guards access to the executable path so that the project is
	// only compiled once.
	compileMtx sync.Mutex

	// executablePath is the path to the compiled executable. This is an empty
	// string until the initial compilation. It should not be accessed directly;
	// use the poetExecutablePath() function instead.
	executablePath string
)

const executableName = "fullnode"

// nodeExecutablePath returns a path to the mock node server executable.
// To ensure the code tests against the most up-to-date version, this method
// compiles a mock node server the first time it is called. After that, the
// generated binary is used for subsequent requests.
func nodeExecutablePath(sourceCodePath string, execBaseDir string) (string, error) {
	compileMtx.Lock()
	defer compileMtx.Unlock()

	// If mock node has already been compiled, just use that.
	if len(executablePath) != 0 {
		return executablePath, nil
	}

	// Build mock node and output an executable in basedir path.
	outputPath := filepath.Join(execBaseDir, executableName)
	if runtime.GOOS == "windows" {
		outputPath += ".exe"
	}
	
	// set command to execute in the same shell.
	// go-spacemesh-mock source code may be outside the working directory
	moveToMNFolder := fmt.Sprintf("cd %v;", sourceCodePath)
	compile := fmt.Sprintf("go build -i -o %v;", outputPath)
	cmd := exec.Command("/bin/sh", "-c", moveToMNFolder + compile)

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("failed to build mock node: %v", err)
	}

	// Save executable path so future calls do not recompile.
	executablePath = outputPath
	return executablePath, nil
}
