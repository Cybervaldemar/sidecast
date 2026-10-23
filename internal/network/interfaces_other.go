//go:build !windows

package network

import (
	"net"
	"os"
	"strconv"
	"strings"
)

func interfacePreferences() map[int]preference {
	result := map[int]preference{}
	routes, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(routes), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		device, err := net.InterfaceByName(fields[0])
		if err != nil {
			continue
		}
		metric, _ := strconv.ParseUint(fields[6], 10, 32)
		result[device.Index] = preference{gateway: true, physical: device.Flags&net.FlagBroadcast != 0, metric: uint32(metric)}
	}
	return result
}
