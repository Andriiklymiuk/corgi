package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
)

// mcpStatusCacheTTL bounds how stale a corgi_status answer may be. Agents poll
// status in tight loops; one probe sweep per second is plenty.
const mcpStatusCacheTTL = time.Second

type fileStamp struct {
	modTime time.Time
	size    int64
}

func stampFile(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{modTime: info.ModTime(), size: info.Size()}
}

func (s fileStamp) equal(o fileStamp) bool {
	return s.size == o.size && s.modTime.Equal(o.modTime)
}

// composeLookup is what a tool call hands loadComposeCtx: the composePath arg
// (often empty), which resolves relative to the cwd, and the env tier.
type composeLookup struct {
	arg  string
	cwd  string
	tier string
}

type composeCacheEntry struct {
	path          string
	compose       fileStamp
	dotenv        fileStamp
	corgi         *utils.CorgiCompose
	unknownFields []string
	duplicateKeys []string
	tierName      string
	tierDir       string
}

func (e *composeCacheEntry) fresh() bool {
	return e.compose.equal(stampFile(e.path)) &&
		e.dotenv.equal(stampFile(filepath.Join(filepath.Dir(e.path), ".env")))
}

type statusCacheEntry struct {
	at      time.Time
	entries []statusEntry
}

type mcpCacheStore struct {
	mu      sync.Mutex
	compose map[composeLookup]*composeCacheEntry
	status  map[string]statusCacheEntry
}

var mcpCache = newMCPCacheStore()

func newMCPCacheStore() *mcpCacheStore {
	return &mcpCacheStore{
		compose: map[composeLookup]*composeCacheEntry{},
		status:  map[string]statusCacheEntry{},
	}
}

// lookupCompose returns a private copy of the cached compose and restores the
// package globals GetCorgiServices would have set, or reports a miss when the
// file (or its sibling .env) changed since it was parsed.
func (c *mcpCacheStore) lookupCompose(key composeLookup) (*utils.CorgiCompose, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.compose[key]
	if !ok {
		return nil, false
	}
	if !e.fresh() {
		delete(c.compose, key)
		return nil, false
	}
	utils.CorgiComposePath = e.path
	utils.CorgiComposePathDir = filepath.Dir(e.path)
	utils.UnknownComposeFields = e.unknownFields
	utils.DuplicateComposeKeys = e.duplicateKeys
	utils.ActiveTierName, utils.ActiveTierDir = e.tierName, e.tierDir
	return cloneCompose(e.corgi), true
}

// storeCompose snapshots a freshly parsed compose plus the globals that came
// with it. Must run right after GetCorgiServices, before the caller mutates it.
func (c *mcpCacheStore) storeCompose(key composeLookup, corgi *utils.CorgiCompose) {
	path := utils.CorgiComposePath
	e := &composeCacheEntry{
		path:          path,
		compose:       stampFile(path),
		dotenv:        stampFile(filepath.Join(filepath.Dir(path), ".env")),
		corgi:         cloneCompose(corgi),
		unknownFields: utils.UnknownComposeFields,
		duplicateKeys: utils.DuplicateComposeKeys,
		tierName:      utils.ActiveTierName,
		tierDir:       utils.ActiveTierDir,
	}
	c.mu.Lock()
	c.compose[key] = e
	c.mu.Unlock()
}

func (c *mcpCacheStore) cachedStatus(path string) ([]statusEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.status[path]
	if !ok || time.Since(e.at) >= mcpStatusCacheTTL {
		return nil, false
	}
	return append([]statusEntry(nil), e.entries...), true
}

func (c *mcpCacheStore) storeStatus(path string, entries []statusEntry) {
	c.mu.Lock()
	c.status[path] = statusCacheEntry{at: time.Now(), entries: append([]statusEntry(nil), entries...)}
	c.mu.Unlock()
}

// invalidateStatus drops every probe result; called after anything that
// changes what is listening (up, down, restore).
func (c *mcpCacheStore) invalidateStatus() {
	c.mu.Lock()
	c.status = map[string]statusCacheEntry{}
	c.mu.Unlock()
}

func (c *mcpCacheStore) reset() {
	c.mu.Lock()
	c.compose = map[composeLookup]*composeCacheEntry{}
	c.status = map[string]statusCacheEntry{}
	c.mu.Unlock()
}

// cloneCompose deep-copies the compose so a handler's in-place edits
// (filterByProfile, workdir overrides) never reach the cached original.
func cloneCompose(c *utils.CorgiCompose) *utils.CorgiCompose {
	if c == nil {
		return nil
	}
	out := deepCopyValue(reflect.ValueOf(c).Elem()).Interface().(utils.CorgiCompose)
	return &out
}

func deepCopyValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Elem().Type())
		out.Elem().Set(deepCopyValue(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(deepCopyValue(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, k := range v.MapKeys() {
			out.SetMapIndex(k, deepCopyValue(v.MapIndex(k)))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			if !out.Field(i).CanSet() {
				// Unexported fields (time.Time and friends): the value copy is all we can do.
				return v
			}
			out.Field(i).Set(deepCopyValue(v.Field(i)))
		}
		return out
	default:
		return v
	}
}
