package ttlcache

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"go.uber.org/goleak"

	"fmt"
	"sync"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// Issue #23: Goroutine leak on closing. When adding a close method i would like to see
// that it can be called in a repeated way without problems.
func TestCache_MultipleCloseCalls(t *testing.T) {
	cache := NewCache()

	cache.SetTTL(time.Millisecond * 100)

	cache.SkipTtlExtensionOnHit(false)
	cache.Set("test", "!")
	startTime := time.Now()
	for now := time.Now(); now.Before(startTime.Add(time.Second * 3)); now = time.Now() {
		if _, found := cache.Get("test"); !found {
			t.Errorf("Item was not found, even though it should not expire.")
		}

	}

	cache.Close()
	cache.Close()
	cache.Close()
	cache.Close()
}

// test for Feature request in issue #12
//
func TestCache_SkipTtlExtensionOnHit(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Millisecond * 100)

	cache.SkipTtlExtensionOnHit(false)
	cache.Set("test", "!")
	startTime := time.Now()
	for now := time.Now(); now.Before(startTime.Add(time.Second * 3)); now = time.Now() {
		if _, found := cache.Get("test"); !found {
			t.Errorf("Item was not found, even though it should not expire.")
		}

	}

	cache.SkipTtlExtensionOnHit(true)
	cache.Set("expireTest", "!")
	// will loop if item does not expire
	for _, found := cache.Get("expireTest"); found; _, found = cache.Get("expireTest") {
	}
}

func TestCache_ForRacesAcrossGoroutines(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Minute * 1)
	cache.SkipTtlExtensionOnHit(false)

	var wgSet sync.WaitGroup
	var wgGet sync.WaitGroup

	n := 500
	wgSet.Add(1)
	go func() {
		for i := 0; i < n; i++ {
			wgSet.Add(1)

			go func(i int) {
				time.Sleep(time.Nanosecond * time.Duration(rand.Int63n(1000000)))
				if i%2 == 0 {
					cache.Set(fmt.Sprintf("test%d", i/10), false)
				} else {
					cache.SetWithTTL(fmt.Sprintf("test%d", i/10), false, time.Second*59)
				}
				wgSet.Done()
			}(i)
		}
		wgSet.Done()
	}()
	wgGet.Add(1)
	go func() {
		for i := 0; i < n; i++ {
			wgGet.Add(1)

			go func(i int) {
				time.Sleep(time.Nanosecond * time.Duration(rand.Int63n(1000000)))
				cache.Get(fmt.Sprintf("test%d", i/10))
				wgGet.Done()
			}(i)
		}
		wgGet.Done()
	}()

	wgGet.Wait()
	wgSet.Wait()
}

func TestCache_SkipTtlExtensionOnHit_ForRacesAcrossGoroutines(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Minute * 1)
	cache.SkipTtlExtensionOnHit(true)

	var wgSet sync.WaitGroup
	var wgGet sync.WaitGroup

	n := 500
	wgSet.Add(1)
	go func() {
		for i := 0; i < n; i++ {
			wgSet.Add(1)

			go func(i int) {
				time.Sleep(time.Nanosecond * time.Duration(rand.Int63n(1000000)))
				if i%2 == 0 {
					cache.Set(fmt.Sprintf("test%d", i/10), false)
				} else {
					cache.SetWithTTL(fmt.Sprintf("test%d", i/10), false, time.Second*59)
				}
				wgSet.Done()
			}(i)
		}
		wgSet.Done()
	}()
	wgGet.Add(1)
	go func() {
		for i := 0; i < n; i++ {
			wgGet.Add(1)

			go func(i int) {
				time.Sleep(time.Nanosecond * time.Duration(rand.Int63n(1000000)))
				cache.Get(fmt.Sprintf("test%d", i/10))
				wgGet.Done()
			}(i)
		}
		wgGet.Done()
	}()

	wgGet.Wait()
	wgSet.Wait()
}

// test github issue #14
// Testing expiration callback would continue with the next item in list, even when it exceeds list lengths
func TestCache_SetCheckExpirationCallback(t *testing.T) {
	iterated := 0
	ch := make(chan struct{})

	cacheAD := NewCache()
	defer cacheAD.Close()

	cacheAD.SetTTL(time.Millisecond)
	cacheAD.SetCheckExpirationCallback(func(key string, value interface{}) bool {
		v := value.(*int)
		t.Logf("key=%v, value=%d\n", key, *v)
		iterated++
		if iterated == 1 {
			// this is the breaking test case for issue #14
			return false
		}
		ch <- struct{}{}
		return true
	})

	i := 2
	cacheAD.Set("a", &i)

	<-ch
}

// test github issue #9
// Due to scheduling the expected TTL of the top entry can become negative (already expired)
// This is an issue because negative TTL at the item level was interpreted as 'use global TTL'
// Which is not right when we become negative due to scheduling.
// This test could use improvement as it's not requiring a lot of time to trigger.
func TestCache_SetExpirationCallback(t *testing.T) {

	type A struct {
	}

	// Setup the TTL cache
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Second * 1)
	cache.SetExpirationCallback(func(key string, value interface{}) {
		t.Logf("This key(%s) has expired\n", key)
	})
	for i := 0; i < 1024; i++ {
		cache.Set(fmt.Sprintf("item_%d", i), A{})
		time.Sleep(time.Millisecond * 10)
		t.Logf("Cache size: %d\n", cache.Count())
	}

	if cache.Count() > 100 {
		t.Fatal("Cache should empty entries >1 second old")
	}
}

// test github issue #4
func TestRemovalAndCountDoesNotPanic(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.Set("key", "value")
	cache.Remove("key")
	count := cache.Count()
	t.Logf("cache has %d keys\n", count)
}

// test github issue #3
func TestRemovalWithTtlDoesNotPanic(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetExpirationCallback(func(key string, value interface{}) {
		t.Logf("This key(%s) has expired\n", key)
	})

	cache.SetWithTTL("keyWithTTL", "value", time.Duration(2*time.Second))
	cache.Set("key", "value")
	cache.Remove("key")

	value, exists := cache.Get("keyWithTTL")
	if exists {
		t.Logf("got %s for keyWithTTL\n", value)
	}
	count := cache.Count()
	t.Logf("cache has %d keys\n", count)

	<-time.After(3 * time.Second)

	value, exists = cache.Get("keyWithTTL")
	if exists {
		t.Logf("got %s for keyWithTTL\n", value)
	} else {
		t.Logf("keyWithTTL has gone")
	}
	count = cache.Count()
	t.Logf("cache has %d keys\n", count)
}

func TestCacheIndividualExpirationBiggerThanGlobal(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	<-time.After(150 * time.Millisecond)
	data, exists := cache.Get("key")
	assert.Equal(t, exists, false, "Expected item to not exist")
	assert.Nil(t, data, "Expected item to be nil")
}

func TestCacheGlobalExpirationByGlobal(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.Set("key", "value")
	<-time.After(50 * time.Millisecond)
	data, exists := cache.Get("key")
	assert.Equal(t, exists, true, "Expected item to exist in cache")
	assert.Equal(t, data.(string), "value", "Expected item to have 'value' in value")

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	data, exists = cache.Get("key")
	assert.Equal(t, exists, true, "Expected item to exist in cache")
	assert.Equal(t, data.(string), "value", "Expected item to have 'value' in value")

	<-time.After(100 * time.Millisecond)
	data, exists = cache.Get("key")
	assert.Equal(t, exists, false, "Expected item to not exist")
	assert.Nil(t, data, "Expected item to be nil")
}

func TestCacheGlobalExpiration(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.Set("key_1", "value")
	cache.Set("key_2", "value")
	<-time.After(200 * time.Millisecond)
	assert.Equal(t, 0, cache.Count(), "Cache should be empty")
	assert.Equal(t, 0, cache.priorityQueue.Len(), "PriorityQueue should be empty")
}

func TestCacheMixedExpirations(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetExpirationCallback(func(key string, value interface{}) {
		t.Logf("expired: %s", key)
	})
	cache.Set("key_1", "value")
	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(150 * time.Millisecond)
	assert.Equal(t, 1, cache.Count(), "Cache should have only 1 item")
}

func TestCacheIndividualExpiration(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	cache.SetWithTTL("key2", "value", time.Duration(100*time.Millisecond))
	cache.SetWithTTL("key3", "value", time.Duration(100*time.Millisecond))
	<-time.After(50 * time.Millisecond)
	assert.Equal(t, cache.Count(), 3, "Should have 3 elements in cache")
	<-time.After(160 * time.Millisecond)
	assert.Equal(t, cache.Count(), 0, "Cache should be empty")

	cache.SetWithTTL("key4", "value", time.Duration(50*time.Millisecond))
	<-time.After(100 * time.Millisecond)
	<-time.After(100 * time.Millisecond)
	assert.Equal(t, 0, cache.Count(), "Cache should be empty")
}

func TestCacheGet(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	data, exists := cache.Get("hello")
	assert.Equal(t, exists, false, "Expected empty cache to return no data")
	assert.Nil(t, data, "Expected data to be empty")

	cache.Set("hello", "world")
	data, exists = cache.Get("hello")
	assert.NotNil(t, data, "Expected data to be not nil")
	assert.Equal(t, true, exists, "Expected data to exist")
	assert.Equal(t, "world", (data.(string)), "Expected data content to be 'world'")
}

func TestCacheGetOrDefault(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	data, err := cache.GetOrDefault("hello", func(key string) (interface{}, error) {
		return "value", nil
	})
	assert.Nil(t, err, "Expected cache to succeed")
	assert.Equal(t, "value", data, "Expected data content to be the default 'value'")

	cache.Set("hello", "world")
	data, err = cache.GetOrDefault("hello", func(key string) (interface{}, error) {
		return "value", nil
	})
	assert.Nil(t, err, "Expected cache to succeed")
	assert.Equal(t, "world", data, "Expected data content to be the last set 'world'")

	cache.Remove("hello")
	data, err = cache.GetOrDefault("hello", func(key string) (interface{}, error) {
		return nil, errors.New("error")
	})
	assert.Error(t, err, "Expected cache to succeed")
	if assert.Error(t, err) {
		assert.Equal(t, errors.New("error"), err)
	}
}

func TestCacheExpirationCallbackFunction(t *testing.T) {
	expiredCount := 0
	var lock sync.Mutex

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(500 * time.Millisecond))
	cache.SetExpirationCallback(func(key string, value interface{}) {
		lock.Lock()
		defer lock.Unlock()
		expiredCount = expiredCount + 1
	})
	cache.SetWithTTL("key", "value", time.Duration(1000*time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(1100 * time.Millisecond)

	lock.Lock()
	defer lock.Unlock()
	assert.Equal(t, 2, expiredCount, "Expected 2 items to be expired")
}

// TestCacheRemoveCallbackFunction ensures the removeCallback is called
// a) when an item is removed
// b) when an item is replaced
// c) when an item is expired
func TestCacheRemoveCallbackFunction(t *testing.T) {
	removedCount := 0
	var lock sync.Mutex

	cache := NewCache()
	defer cache.Close()

	cache.SetRemoveCallback(func(key string, value interface{}) {
		lock.Lock()
		defer lock.Unlock()
		removedCount = removedCount + 1
	})

	cache.Set("key_1", "value")
	// this calls removeCallback
	cache.Remove("key_1")

	cache.Set("key_1", "value")
	// this calls removeCallback
	cache.Set("key_1", "value2")

	// this calls removeCallback on expiry
	cache.SetWithTTL("key", "value", time.Duration(1000*time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(1100 * time.Millisecond)

	lock.Lock()
	defer lock.Unlock()
	assert.Equal(t, 3, removedCount, "Expected 3 items to be removed")
}

// TestCacheCheckExpirationCallbackFunction should consider that the next entry in the queue
// needs to be considered for eviction even if the callback returns no eviction for the current item
func TestCacheCheckExpirationCallbackFunction(t *testing.T) {
	expiredCount := 0
	var lock sync.Mutex

	cache := NewCache()
	defer cache.Close()

	cache.SkipTtlExtensionOnHit(true)
	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetCheckExpirationCallback(func(key string, value interface{}) bool {
		if key == "key2" || key == "key4" {
			return true
		}
		return false
	})
	cache.SetExpirationCallback(func(key string, value interface{}) {
		lock.Lock()
		expiredCount = expiredCount + 1
		lock.Unlock()
	})
	cache.Set("key", "value")
	cache.Set("key3", "value")
	cache.Set("key2", "value")
	cache.Set("key4", "value")

	<-time.After(110 * time.Millisecond)
	lock.Lock()
	assert.Equal(t, 2, expiredCount, "Expected 2 items to be expired")
	lock.Unlock()
}

func TestCacheNewItemCallbackFunction(t *testing.T) {
	newItemCount := 0
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetNewItemCallback(func(key string, value interface{}) {
		newItemCount = newItemCount + 1
	})
	cache.Set("key", "value")
	cache.Set("key2", "value")
	cache.Set("key", "value")
	<-time.After(110 * time.Millisecond)
	assert.Equal(t, 2, newItemCount, "Expected only 2 new items")
}

func TestCacheRemove(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(70 * time.Millisecond)
	removeKey := cache.Remove("key")
	removeKey2 := cache.Remove("key_2")
	assert.Equal(t, true, removeKey, "Expected 'key' to be removed from cache")
	assert.Equal(t, false, removeKey2, "Expected 'key_2' to already be expired from cache")
}

func TestCacheSetWithTTLExistItem(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(50*time.Millisecond))
	<-time.After(30 * time.Millisecond)
	cache.SetWithTTL("key", "value2", time.Duration(50*time.Millisecond))
	data, exists := cache.Get("key")
	assert.Equal(t, true, exists, "Expected 'key' to exist")
	assert.Equal(t, "value2", data.(string), "Expected 'data' to have value 'value2'")
}

func TestCache_Purge(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(100 * time.Millisecond))

	for i := 0; i < 5; i++ {

		cache.SetWithTTL("key", "value", time.Duration(50*time.Millisecond))
		<-time.After(30 * time.Millisecond)
		cache.SetWithTTL("key", "value2", time.Duration(50*time.Millisecond))
		cache.Get("key")

		cache.Purge()
		assert.Equal(t, 0, cache.Count(), "Cache should be empty")
	}

}
