package fileutil

import (
	"os"
	"path/filepath"
)

func Find(path string) (*os.File, error) {
	exec, _ := os.Executable()
	configFile := filepath.Join(filepath.Dir(exec), path)
	file, err := os.Open(configFile)
	return file, err
}