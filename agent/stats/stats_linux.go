//go:build linux

package stats

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

func readCPUTimes() (idle, total uint64, err error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		for index, field := range strings.Fields(line)[1:] {
			value, parseErr := strconv.ParseUint(field, 10, 64)
			if parseErr != nil {
				return 0, 0, parseErr
			}
			total += value
			if index == 3 || index == 4 {
				idle += value
			}
		}
		return idle, total, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	return 0, 0, errors.New("stats: no cpu line in /proc/stat")
}

func readMemory() (used, total uint64, err error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = file.Close() }()

	var available, free uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		kilobytes, parseErr := meminfoKilobytes(value)
		if parseErr != nil {
			continue
		}
		switch key {
		case "MemTotal":
			total = kilobytes
		case "MemFree":
			free = kilobytes
		case "MemAvailable":
			available = kilobytes
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if total == 0 {
		return 0, 0, errors.New("stats: no MemTotal in /proc/meminfo")
	}
	reclaimable := available
	if reclaimable == 0 {
		reclaimable = free
	}
	if reclaimable > total {
		reclaimable = total
	}
	return total - reclaimable, total, nil
}

func meminfoKilobytes(value string) (uint64, error) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return 0, errors.New("stats: empty meminfo value")
	}
	amount, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, err
	}
	return amount * 1024, nil
}

func osVersion() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readNetwork() (rx, tx uint64, err error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = file.Close() }()
	return parseNetDev(file)
}

func readDiskIO() (read, write uint64, err error) {
	devices, err := wholeBlockDevices()
	if err != nil {
		return 0, 0, err
	}
	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = file.Close() }()
	return parseDiskStats(file, devices)
}

func wholeBlockDevices() (map[string]struct{}, error) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil, err
	}
	devices := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if pseudoBlockDevice(name) {
			continue
		}
		devices[name] = struct{}{}
	}
	if len(devices) == 0 {
		return nil, errUnsupported
	}
	return devices, nil
}
