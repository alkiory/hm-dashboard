package collector

import (
	"context"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// CollectHardware reads host metrics via gopsutil. All operations are bounded
// by a short context so a slow /proc can never stall the poller.
// Exported so the poller package can call it.
func CollectHardware(ctx context.Context) (Hardware, error) {
	var h Hardware

	// CPU percent needs delta sampling; the first reading establishes the
	// baseline. We pass interval=0 to do a non-blocking sample — this is the
	// lightest weight call and matches the cadence of our outer polling tick.
	if percents, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(percents) > 0 {
		h.CPUPercent = round1(percents[0])
	}
	if cores, err := cpu.CountsWithContext(ctx, false); err == nil {
		h.CPUCores = cores
	}

	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		h.MemoryUsed = v.Used
		h.MemoryTotal = v.Total
		h.MemoryPercent = round1(v.UsedPercent)
	}
	if v, err := mem.SwapMemoryWithContext(ctx); err == nil {
		h.SwapUsed = v.Used
		h.SwapTotal = v.Total
	}

	// Disks: skip virtual filesystems (tmpfs, overlay, devtmpfs, squashfs)
	// to keep the list short and useful.
	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		seen := map[string]bool{}
		for _, p := range parts {
			if skipFS(p.Fstype) {
				continue
			}
			key := p.Mountpoint
			if seen[key] {
				continue
			}
			seen[key] = true
			usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
			if err != nil || usage.Total == 0 {
				continue
			}
			h.Disks = append(h.Disks, Disk{
				Path:    p.Mountpoint,
				Used:    usage.Used,
				Total:   usage.Total,
				Percent: round1(usage.UsedPercent),
				FSType:  p.Fstype,
			})
		}
	}

	// Temperatures: prefer the linux thermal_zone files (cheap and reliable
	// on most NAS/homelab boards). gopsutil's sensors package depends on
	// optional tooling that we don't want to force into the image.
	if t, ok := readLinuxTemperature(ctx); ok {
		h.TemperatureC = round1(t)
		h.HasTemp = true
	}

	if info, err := host.InfoWithContext(ctx); err == nil {
		h.Hostname = info.Hostname
		h.OS = info.Platform + " " + info.PlatformVersion
		h.UptimeSec = info.Uptime
	} else {
		// Last-ditch fallback: derive uptime from /proc using nucleus of host.
		if bt, err := host.BootTimeWithContext(ctx); err == nil {
			h.UptimeSec = uint64(time.Now().Unix()) - bt
		}
	}

	return h, nil
}

// skipFS returns true for virtual filesystems that should not be reported
// on the disks card. Names mirror gopsutil's reported Fstype.
func skipFS(fs string) bool {
	switch strings.ToLower(fs) {
	case "tmpfs", "devtmpfs", "overlay", "squashfs", "proc", "sysfs",
		"cgroup", "cgroup2", "devpts", "mqueue", "autofs", "binfmt_misc",
		"securityfs", "debugfs", "fusectl", "pstore", "ramfs", "hugetlbfs",
		"configfs", "nsfs", "fuse.gvfsd-fuse":
		return true
	}
	return false
}

// round1 trims a percentage to one decimal place to reduce payload and
// UI jitter. The rounding keeps cards visually stable across refreshes.
func round1(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return float64(int(v*10+0.5)) / 10
}

// readLinuxTemperature queries common Linux thermal sensor files. Bounded by
// ctx so the poller can never block on a broken sysfs.
func readLinuxTemperature(ctx context.Context) (float64, bool) {
	type result struct {
		v  float64
		ok bool
	}
	resCh := make(chan result, 1)

	go func() {
		defer func() { _ = recover() }()
		var max float64
		max = maxTemperature(max, "/sys/class/thermal", "thermal_zone", "/temp")
		max = maxTemperature(max, "/sys/class/hwmon", "hwmon", "")
		if max > 0 {
			resCh <- result{v: max, ok: true}
			return
		}
		resCh <- result{}
	}()

	select {
	case <-ctx.Done():
		return 0, false
	case r := <-resCh:
		return r.v, r.ok
	}
}

func maxTemperature(current float64, dir, prefix, suffix string) float64 {
	entries, err := readDirNames(dir)
	if err != nil {
		return current
	}
	for _, name := range entries {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if suffix != "" {
			current = maxSensorTemp(current, dir+"/"+name+suffix)
			continue
		}
		files, err := readDirNames(dir + "/" + name)
		if err != nil {
			continue
		}
		for _, file := range files {
			if strings.HasPrefix(file, "temp") && strings.HasSuffix(file, "_input") {
				current = maxSensorTemp(current, dir+"/"+name+"/"+file)
			}
		}
	}
	return current
}

func maxSensorTemp(current float64, path string) float64 {
	raw, err := readFile(path)
	if err != nil {
		return current
	}
	milli, err := parseInt(strings.TrimSpace(raw))
	if err != nil || milli <= 0 {
		return current
	}
	t := float64(milli) / 1000.0
	if t < 1 || t > 130 {
		return current
	}
	if t > current {
		return t
	}
	return current
}

// Convenience helpers wrapping os functions so thermal reading can be
// tested or stubbed without rewriting paths.
var (
	readDirNames = func(p string) ([]string, error) {
		f, err := openDir(p)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return f.Readdirnames(-1)
	}
	readFile = func(p string) (string, error) {
		b, err := readAll(p)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
)

// parseInt is split out so unit tests can validate parsing logic without
// reaching for the real sysfs in CI environments.
var parseInt = func(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadInt
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

var errBadInt = &intParseError{}

// intParseError is the sentinel value returned by parseInt for non-digit input.
type intParseError struct{}

func (e *intParseError) Error() string { return "invalid integer" }
