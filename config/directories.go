package config

import (
	"os"
	"os/user"
	"path"
	"strings"
)

// Directory and paths helpers

func GetUserHomeDirectory() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if usr, err := user.Current(); err == nil {
		return usr.HomeDir
	}
	return ""
}

// Retursn an os-specific full path:
// - replace ~ with user's home dir path
// - expand any ${vars} or $vars
// - resolve relative paths /.../
// p: source path name
func GetCanonicalPath(p string) string {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		if home := GetUserHomeDirectory(); home != "" {
			p = home + p[1:]
		}
	}
	return path.Clean(os.ExpandEnv(p))
}
