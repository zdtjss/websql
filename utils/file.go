package utils

import (
	"os"
	"path/filepath"
)

func Find(path string) *os.File {
	exec, err := os.Executable()
	Println(err)
	configFile := filepath.Join(filepath.Dir(exec), path)
	file, err := os.Open(configFile)
	Println(err)
	return file
}
