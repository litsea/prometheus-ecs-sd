package cache

import (
	"errors"
	"sync"
	"time"
)

// Cache stores arbitrary data with expiration time.
type Cache struct {
	items sync.Map
	close chan struct{}

	returnStale       bool
	janitorInterval   time.Duration
	defaultExpiration time.Duration
}

// An item represents arbitrary data with expiration time.
type item struct {
	data    any
	expires int64
}

type Option func(*Cache) error

func WithJanitor(interval time.Duration) Option {
	return func(c *Cache) error {
		if interval <= 0 {
			return errors.New("janitor interval must be greater than 0")
		}
		c.janitorInterval = interval
		c.close = make(chan struct{})
		return nil
	}
}

func WithGetReturnStale() Option {
	return func(c *Cache) error {
		c.returnStale = true
		return nil
	}
}

func WithDefaultExpiration(expiration time.Duration) Option {
	return func(c *Cache) error {
		c.defaultExpiration = expiration
		return nil
	}
}

func New(opts ...Option) *Cache {
	cache := &Cache{
		janitorInterval:   -1,
		defaultExpiration: -1,
	}

	for _, opt := range opts {
		_ = opt(cache)
	}

	if cache.janitorInterval > 0 {
		go cache.janitor()
	}

	return cache
}

func (c *Cache) janitor() {
	ticker := time.NewTicker(c.janitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now().UnixNano()

			c.items.Range(func(key, value any) bool {
				it := value.(item) //nolint:forcetypeassert

				if it.expires > 0 && now > it.expires {
					c.items.Delete(key)
				}

				return true
			})

		case <-c.close:
			return
		}
	}
}

// Get gets the value for the given key.
func (c *Cache) Get(key any) (any, bool) {
	obj, exists := c.items.Load(key)

	if !exists {
		return nil, false
	}

	it := obj.(item) //nolint:forcetypeassert

	if it.expires > 0 && time.Now().UnixNano() > it.expires {
		if c.returnStale {
			return it.data, false
		}

		return nil, false
	}

	return it.data, true
}

// Set sets a value for the given key with the specified expiration duration.
// If the duration is less than 0, the value never expires.
func (c *Cache) Set(key, value any, duration time.Duration) {
	var expires int64

	if duration > 0 {
		expires = time.Now().Add(duration).UnixNano()
	}

	c.items.Store(key, item{
		data:    value,
		expires: expires,
	})
}

// SetDefault sets a value for the given key with the default expiration duration.
func (c *Cache) SetDefault(key, value any) {
	c.Set(key, value, c.defaultExpiration)
}

// Range calls f sequentially for each key and value present in the cache.
// If f returns false, range stops the iteration.
func (c *Cache) Range(f func(key, value any) bool) {
	now := time.Now().UnixNano()

	fn := func(key, value any) bool {
		it := value.(item) //nolint:forcetypeassert

		if it.expires > 0 && now > it.expires {
			return true
		}

		return f(key, it.data)
	}

	c.items.Range(fn)
}

// Delete deletes the key and its value from the cache.
func (c *Cache) Delete(key any) {
	c.items.Delete(key)
}

// Close closes the cache and frees up resources.
func (c *Cache) Close() {
	c.close <- struct{}{}
	c.items = sync.Map{}
}
