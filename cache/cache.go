package cache

import (
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

var (
	cMu   = sync.Mutex{}
	cList = make(map[string]*Controller)
)

func MustRegister(configs ...*config.Cache) {
	if len(configs) == 0 {
		return
	}
	for _, cfg := range configs {
		if _, ok := cList[cfg.Name]; ok {
			log.Fatalf("cache %q already registered", cfg.Name)
		}
		c := &Controller{
			Name:     cfg.Name,
			MaxSize:  int64(cfg.MaxSize),
			Dir:      cfg.Dir,
			Expire:   cfg.Expire,
			registry: make(map[string]time.Time),
		}
		if err := c.Run(); err != nil {
			log.Fatalf("cache %q error: %s", cfg.Name, err)
		}
		cList[c.Name] = c
	}
}

func CacheController(name string) *Controller {
	cMu.Lock()
	defer cMu.Unlock()
	return cList[name]
}

type Controller struct {
	Dir, Name string
	Expire    time.Duration
	MaxSize   int64

	mu       sync.Mutex
	registry map[string]time.Time
	size     int64
}

const cleanUpInterval = time.Second * 20

// Runs a goroutine to watch limits exceeding.
// Also re-reads already cached files to refresh registry after reload
func (c *Controller) Run() error {
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		return fmt.Errorf("error while creating folder %q: %s", c.Dir, err)
	}

	walkFn := func(_ string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			c.size += info.Size()
			c.registry[info.Name()] = info.ModTime()
		}
		return err
	}
	if err := filepath.Walk(c.Dir, walkFn); err != nil {
		return fmt.Errorf("error while reading folder %q: %s", c.Dir, err)
	}
	c.cleanup()
	go func() {
		time.Sleep(cleanUpInterval)
		c.cleanup()
	}()
	return nil
}

func (c Controller) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	_, ok := c.registry[key]
	if !ok {
		c.mu.Unlock()
		return nil, false
	}
	path := fmt.Sprintf("%s/%s", c.Dir, key)
	name := c.Name
	c.mu.Unlock()

	resp, err := ioutil.ReadFile(path)
	if err != nil {
		log.Errorf("err while reading file %q for cache %q: %s", key, name, err)
		return nil, false
	}
	return resp, true
}

// TODO: somehow release lock while file is creating and writing to minimize wasted time
func (c *Controller) Store(key string, resp *http.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// exit if such key is already present in registry
	if _, ok := c.registry[key]; ok {
		return
	}
	path := fmt.Sprintf("%s/%s", c.Dir, key)
	f, err := os.Create(path)
	if err != nil {
		log.Errorf("err while creating file %q for cache %q: %s", f.Name(), c.Name, err)
		return
	}
	if err != resp.Write(f) {
		log.Errorf("err while writing into file %q for cache %q: %s", f.Name(), c.Name, err)
		return
	}
	info, err := f.Stat()
	if err != nil {
		log.Errorf("err while reading file %q for cache %q: %s", f.Name(), c.Name, err)
		f.Close()
		return
	}
	c.registry[key] = info.ModTime()
	c.size += info.Size()
	f.Close()
}

func (c *Controller) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// if cache is empty - exit
	if c.size == 0 {
		return
	}

	leftBound := time.Now().Add(-c.Expire)
	for key, mod := range c.registry {
		if mod.Before(leftBound) {
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
	for name, mod := range c.registry {
		fileList = append(fileList, &file{
			name: name,
			mod:  mod,
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
	path := fmt.Sprintf("%s/%s", c.Dir, key)
	f, err := os.Stat(path)
	if err != nil {
		log.Errorf("error while getting file %q info for cache %q: %s", path, c.Name, err)
		return
	}
	size := f.Size()
	if err := os.Remove(path); err != nil {
		log.Errorf("error while removing file %q for cache %q: %s", path, c.Name, err)
		return
	}

	delete(c.registry, key)
	c.size -= size
}
