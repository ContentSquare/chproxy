package cache

import (
	"fmt"
	"io"
	"os"
)

// walkDir calls f on all the cache files in the given dir.
func walkDir(dir string, f func(fi os.FileInfo)) error {
	// Do not use filepath.Walk, since it is inefficient
	// for large number of files.
	// See https://golang.org/pkg/path/filepath/#Walk .
	fd, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot open %q: %s", dir, err)
	}
	defer fd.Close()

	for {
		fis, err := fd.Readdir(1024)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("cannot read files in %q: %s", dir, err)
		}
		for _, fi := range fis {
			if fi.IsDir() {
				// Skip subdirectories
				continue
			}
			fn := fi.Name()
			if !cachefileRegexp.MatchString(fn) {
				// Skip invalid filenames
				continue
			}
			f(fi)
		}
	}
}
