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

	"andriiklymiuk/corgi/utils/atomicfile"
)

var IsolateLease string

const (
	leasePortStride = 100
	leaseMaxSlots   = 20
)

var leaseNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

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
		if db.Port, err = shiftPort(db.Port, lease.Offset); err != nil {
			return fmt.Errorf("--isolate %s: %v", name, err)
		}
		if db.Port2, err = shiftPort(db.Port2, lease.Offset); err != nil {
			return fmt.Errorf("--isolate %s: %v", name, err)
		}
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
		if svc.Port, err = shiftPort(svc.Port, lease.Offset); err != nil {
			return fmt.Errorf("--isolate %s: %v", name, err)
		}
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

func shiftPort(port, offset int) (int, error) {
	if port == 0 {
		return 0, nil
	}
	shifted := port + offset
	if shifted > 65535 {
		return 0, fmt.Errorf("port %d shifted by +%d is %d, past the top of the port range", port, offset, shifted)
	}
	return shifted, nil
}

func leaseSlug(name string) string {
	return strings.ToLower(strings.NewReplacer(".", "-", "_", "-").Replace(name))
}

func loadOrCreateLease(name string) (Lease, error) {
	existing, err := ReadLease(name)
	if err == nil {
		return existing, nil
	}
	if !os.IsNotExist(err) {
		return Lease{}, fmt.Errorf("lease %q is unreadable: %v", name, err)
	}
	return claimLeaseSlot(name)
}

func claimLeaseSlot(name string) (Lease, error) {
	dir := LeasesDir(CorgiComposePathDir)
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return Lease{}, mkErr
	}
	EnsureCorgiServicesIgnore(filepath.Dir(dir), ".leases")

	taken := map[int]bool{}
	for _, lease := range ListLeases() {
		taken[lease.Offset] = true
	}
	for slot := 1; slot <= leaseMaxSlots; slot++ {
		offset := slot * leasePortStride
		if taken[offset] {
			continue
		}
		claim, err := os.OpenFile(slotClaimPath(dir, offset), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return Lease{}, err
		}
		_, _ = claim.WriteString(name)
		_ = claim.Close()
		Infof("new lease %q: ports shifted by +%d (corgi leases release %s to drop it)\n", name, offset, name)
		return Lease{Name: name, Offset: offset, CreatedAt: time.Now().UTC()}, nil
	}
	return Lease{}, fmt.Errorf("no free port block left: %d leases already exist (corgi leases)", leaseMaxSlots)
}

func slotClaimPath(dir string, offset int) string {
	return filepath.Join(dir, fmt.Sprintf(".slot-%d", offset))
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
	return atomicfile.Write(leasePath(lease.Name), data, 0o644)
}

func ListLeases() []Lease {
	entries, err := os.ReadDir(LeasesDir(CorgiComposePathDir))
	if err != nil {
		return nil
	}
	var leases []Lease
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasPrefix(entry.Name(), ".") {
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
	if !leaseNameRe.MatchString(name) {
		return fmt.Errorf("lease %q: use letters, digits, dot, dash or underscore (max 40)", name)
	}
	lease, err := ReadLease(name)
	if err != nil {
		return fmt.Errorf("no lease named %q", name)
	}
	_ = os.Remove(slotClaimPath(LeasesDir(CorgiComposePathDir), lease.Offset))
	return os.Remove(leasePath(name))
}
