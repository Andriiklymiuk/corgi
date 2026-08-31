package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// IsolateLease is the --isolate value: empty means no isolation at all.
var IsolateLease string

const (
	leasePortStride = 100
	leaseMaxSlots   = 20
)

var leaseNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

type Lease struct {
	Name       string            `json:"name"`
	Offset     int               `json:"portOffset"`
	CreatedAt  time.Time         `json:"createdAt"`
	Ports      map[string]int    `json:"ports,omitempty"`
	Databases  map[string]string `json:"databases,omitempty"`
	Containers string            `json:"containerScope,omitempty"`
}

func LeasesDir(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", ".leases")
}

// ApplyIsolationLease shifts every declared port by the lease's own offset,
// suffixes each database name, and scopes container names, so a second agent
// can run the same stack in the same directory without colliding.
func ApplyIsolationLease(c *CorgiCompose, name string) error {
	if c == nil || name == "" {
		return nil
	}
	if !leaseNameRe.MatchString(name) {
		return fmt.Errorf("--isolate %q: use letters, digits, dot, dash or underscore (max 40)", name)
	}
	lease, err := loadOrCreateLease(name)
	if err != nil {
		return err
	}

	lease.Ports = map[string]int{}
	lease.Databases = map[string]string{}
	for i := range c.DatabaseServices {
		db := &c.DatabaseServices[i]
		db.Port = shiftPort(db.Port, lease.Offset)
		db.Port2 = shiftPort(db.Port2, lease.Offset)
		if db.DatabaseName != "" {
			db.DatabaseName = db.DatabaseName + "_" + leaseSlug(name)
			lease.Databases[db.ServiceName] = db.DatabaseName
		}
		if db.Port != 0 {
			lease.Ports[db.ServiceName] = db.Port
		}
	}
	for i := range c.Services {
		svc := &c.Services[i]
		svc.Port = shiftPort(svc.Port, lease.Offset)
		if svc.Port != 0 {
			lease.Ports[svc.ServiceName] = svc.Port
		}
	}

	c.ScopeContainers = true
	SetContainerScope(c)
	containerScope = DockerSafeName(ScopedContainerBase(leaseSlug(name)))
	lease.Containers = containerScope

	return writeLease(lease)
}

func shiftPort(port, offset int) int {
	if port == 0 {
		return 0
	}
	return port + offset
}

func leaseSlug(name string) string {
	return strings.ToLower(strings.NewReplacer(".", "-", "_", "-").Replace(name))
}

func loadOrCreateLease(name string) (Lease, error) {
	if existing, err := ReadLease(name); err == nil {
		return existing, nil
	}
	taken := map[int]bool{}
	for _, lease := range ListLeases() {
		taken[lease.Offset] = true
	}
	for slot := 1; slot <= leaseMaxSlots; slot++ {
		offset := slot * leasePortStride
		if !taken[offset] {
			return Lease{Name: name, Offset: offset, CreatedAt: time.Now().UTC()}, nil
		}
	}
	return Lease{}, fmt.Errorf("no free port block left: %d leases already exist (corgi leases)", leaseMaxSlots)
}

func leasePath(name string) string {
	return filepath.Join(LeasesDir(CorgiComposePathDir), name+".json")
}

func ReadLease(name string) (Lease, error) {
	var lease Lease
	data, err := os.ReadFile(leasePath(name))
	if err != nil {
		return lease, err
	}
	if err := json.Unmarshal(data, &lease); err != nil {
		return lease, err
	}
	return lease, nil
}

func writeLease(lease Lease) error {
	dir := LeasesDir(CorgiComposePathDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	EnsureCorgiServicesIgnore(filepath.Dir(dir), ".leases")
	data, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(leasePath(lease.Name), data, 0o644)
}

func ListLeases() []Lease {
	entries, err := os.ReadDir(LeasesDir(CorgiComposePathDir))
	if err != nil {
		return nil
	}
	var leases []Lease
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if lease, err := ReadLease(name); err == nil {
			leases = append(leases, lease)
		}
	}
	sort.Slice(leases, func(i, j int) bool { return leases[i].Offset < leases[j].Offset })
	return leases
}

func ReleaseLease(name string) error {
	if _, err := ReadLease(name); err != nil {
		return fmt.Errorf("no lease named %q", name)
	}
	return os.Remove(leasePath(name))
}
