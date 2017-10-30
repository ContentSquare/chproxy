package cache

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
)

var cList = make(map[string]*Controller)

func MustRegister(cfgs ...config.Cache) {
	if len(cfgs) == 0 {
		return
	}

	for _, cfg := range cfgs {
		if _, ok := cList[cfg.Name]; ok {
			log.Fatalf("cache controller %q is already registered", cfg.Name)
		}
		if len(cfg.Dir) == 0 {
			log.Fatalf("dir cannot be empty")
		}
		if cfg.MaxSize == 0 {
			log.Fatalf("max_size cannot be 0")
		}
		if cfg.Expire == 0 {
			log.Fatalf("expire cannot be 0")
		}
		c := &Controller{
			Name:     cfg.Name,
			MaxSize:  int64(cfg.MaxSize),
			Dir:      cfg.Dir,
			Expire:   cfg.Expire,
			registry: make(map[string]file),
		}
		if err := c.Run(); err != nil {
			log.Fatalf("cache %q error: %s", cfg.Name, err)
		}
		cList[cfg.Name] = c
	}
}

func GetController(name string) *Controller {
	return cList[name]
}

type Controller struct {
	Dir, Name string
	Expire    time.Duration
	MaxSize   int64

	mu       sync.Mutex
	registry map[string]file
	size     int64
}

type file struct {
	mod  time.Time
	size int64
}

// Runs a goroutine to watch limits exceeding.
// Also re-reads already cached files
// with names of proper length to refresh registry after reload
func (c *Controller) Run() error {
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		return fmt.Errorf("error while creating folder %q: %s", c.Dir, err)
	}
	walkFn := func(_ string, info os.FileInfo, err error) error {
		if !info.IsDir() || len(info.Name()) == sha1.Size {
			c.add(info)
		}
		return err
	}
	if err := filepath.Walk(c.Dir, walkFn); err != nil {
		return fmt.Errorf("error while reading folder %q: %s", c.Dir, err)
	}
	c.cleanup()
	go func() {
		for {
			time.Sleep(cleanUpInterval)
			c.cleanup()
		}
	}()
	return nil
}

const cleanUpInterval = time.Minute

func (c *Controller) NewResponseWriter(rw http.ResponseWriter) (*ResponseWriter, error) {
	f, err := ioutil.TempFile(c.Dir, "temp")
	if err != nil {
		return nil, fmt.Errorf("error while creating temp-file: %s", err)
	}
	return &ResponseWriter{
		ResponseWriter: rw,
		file:           f,
	}, nil
}

func (c *Controller) Flush(key string, rw *ResponseWriter) error {
	rw.file.Close()
	oldName := rw.file.Name()
	newName := fmt.Sprintf("%s/%s", c.Dir, key)
	err := os.Rename(oldName, newName)
	if err != nil {
		return fmt.Errorf("error while renaming file %q for cache %q: %s", oldName, c.Name, err)
	}

	info, err := os.Stat(newName)
	if err != nil {
		return fmt.Errorf("err while reading file %q for cache %q: %s", newName, c.Name, err)
	}

	c.mu.Lock()
	c.add(info)
	c.mu.Unlock()
	return nil
}

func (c *Controller) WriteTo(rw http.ResponseWriter, key string) (int64, error) {
	if len(key) == 0 {
		return 0, fmt.Errorf("empty cache key passed")
	}

	c.mu.Lock()
	// exit if such key is already present in registry
	file, ok := c.registry[key]
	if !ok {
		c.mu.Unlock()
		return 0, fmt.Errorf("cache key %q is absent in cache", key)
	}
	if file.mod.Before(time.Now().Add(-c.Expire)) {
		c.remove(key)
		c.mu.Unlock()
		return 0, fmt.Errorf("cache key %q expired", key)
	}
	c.mu.Unlock()

	filePath := fmt.Sprintf("%s/%s", c.Dir, key)
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		c.mu.Lock()
		c.remove(key)
		c.mu.Unlock()
		log.Errorf("error while reading file %q: %s", filePath, err)
	}
	b := bytes.NewBuffer(data)
	return b.WriteTo(rw)
}

func (c *Controller) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// if cache is empty - exit
	if c.size < 1 {
		return
	}
	leftBound := time.Now().Add(-c.Expire)
	for key, f := range c.registry {
		if f.mod.Before(leftBound) {
			c.remove(key)
		}
	}
	// if size limits are fine - exit
	if c.size <= c.MaxSize {
		return
	}

	type file struct {
		name string
		mod  time.Time
	}
	var fileList []*file
	for name, f := range c.registry {
		fileList = append(fileList, &file{
			name: name,
			mod:  f.mod,
		})
	}

	sort.Slice(fileList, func(i, j int) bool {
		return fileList[i].mod.Before(fileList[j].mod)
	})
	i := 0
	for {
		if c.size < c.MaxSize {
			break
		}
		c.remove(fileList[i].name)
		i++
	}
}

// remove is not concurrent safe and must be called only under lock
func (c *Controller) remove(key string) {
	file := c.registry[key]
	delete(c.registry, key)
	c.size -= file.size
	path := fmt.Sprintf("%s/%s", c.Dir, key)
	if err := os.Remove(path); err != nil {
		log.Errorf("error while removing file %q for cache %q: %s", path, c.Name, err)
		return
	}
}

func (c *Controller) add(info os.FileInfo) {
	size := info.Size()
	c.size += size
	c.registry[info.Name()] = file{
		// we use time.Now() instead of file.ModeTime() because
		// different OS may store this time with different precision
		mod:  time.Now(),
		size: size,
	}
}

// cachedResponseWriter supposed to intercept writing
// and redirect it into provided writer
type ResponseWriter struct {
	file *os.File
	http.ResponseWriter
}

func (rw *ResponseWriter) Write(b []byte) (int, error) { return rw.file.Write(b) }

func (rw *ResponseWriter) Bytes() ([]byte, error) { return ioutil.ReadFile(rw.file.Name()) }

func (rw *ResponseWriter) Delete() error { return os.Remove(rw.file.Name()) }
