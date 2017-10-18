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

func MustRegister(configs ...*config.Cache) {
	if len(configs) == 0 {
		return
	}
	for _, cfg := range configs {
		c := &Cache{
			Name:     cfg.Name,
			MaxSize:  int64(cfg.MaxSize),
			Dir:      cfg.Dir,
			Expire:   cfg.Expire,
			registry: make(map[string]time.Time),
		}
		if err := c.Run(); err != nil {
			log.Fatalf("cache %q error: %s", cfg.Name, err)
		}
	}
}

type Cache struct {
	Dir, Name string
	Expire    time.Duration
	MaxSize   int64

	mu       *sync.Mutex
	registry map[string]time.Time
	size     int64
}

const cleanUpInterval = time.Minute * 5

func (c *Cache) Run() error {
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		return fmt.Errorf("error while creating folder %q: %s", c.Dir, err)
	}
	err := filepath.Walk(c.Dir, func(_ string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			c.size += info.Size()
			c.registry[info.Name()] = info.ModTime()
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("error while reading folder %q: %s", c.Dir, err)
	}

	c.cleanup()
	go func() {
		time.Sleep(cleanUpInterval)
		c.cleanup()
	}()
	return nil
}

func (c Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	f, ok := c.registry[key]
	if !ok {
		c.mu.Unlock()
		return nil, false
	}
	path := fmt.Sprintf("%s/%s", c.Dir, f)
	name := c.Name
	c.mu.Unlock()

	resp, err := ioutil.ReadFile(path)
	if err != nil {
		log.Errorf("err while reading file %q for cache %q: %s", f, name, err)
		return nil, false
	}
	return resp, true
}

func (c Cache) Store(key string, resp *http.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.registry[key]; ok {
		return
	}
	path := fmt.Sprintf("%s/%s", c.Dir, key)
	f, err := os.Create(path)
	if err != nil {
		log.Errorf("err while creating file %q for cache %q: %s", f, c.Name, err)
		return
	}
	if err != resp.Write(f) {
		log.Errorf("err while writing into file %q for cache %q: %s", f, c.Name, err)
		return
	}
	f.Close()
	c.registry[key] = time.Now()
}

func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	leftBound := time.Now().Add(-c.Expire)
	for name, mod := range c.registry {
		if mod.Before(leftBound) {
			c.remove(name)
		}
	}
	if c.size > c.MaxSize {
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
			if c.size > c.MaxSize {
				c.remove(fileList[i].name)
			}
			i++
		}
	}
}

func (c *Cache) remove(file string) {
	path := fmt.Sprintf("%s/%s", c.Dir, file)
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
	delete(c.registry, file)
	c.size -= size
}
