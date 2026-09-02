package catalog

import "os"

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
