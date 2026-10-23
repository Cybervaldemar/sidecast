// Package network discovers addresses from active interfaces, without an
// address cache, external probes, or assumptions about a particular subnet.
package network

import (
	"net"
	"sort"
)

type preference struct {
	gateway  bool
	physical bool
	metric   uint32
}

type candidate struct {
	ip    net.IP
	index int
	preference
}

func Addresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []string{}
	}
	preferences := interfacePreferences()
	var candidates []candidate
	for _, device := range interfaces {
		if device.Flags&net.FlagUp == 0 || device.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := device.Addrs()
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				candidates = append(candidates, candidate{ip: ip, index: device.Index, preference: preferences[device.Index]})
			}
		}
	}
	return ordered(candidates)
}

func ordered(candidates []candidate) []string {
	rank := func(c candidate) int {
		switch {
		case c.gateway && c.physical:
			return 0
		case c.gateway:
			return 1
		case c.physical:
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if (a.ip.To4() != nil) != (b.ip.To4() != nil) {
			return a.ip.To4() != nil
		}
		if a.metric != b.metric {
			return a.metric < b.metric
		}
		if a.ip.IsPrivate() != b.ip.IsPrivate() {
			return a.ip.IsPrivate()
		}
		if a.index != b.index {
			return a.index < b.index
		}
		return a.ip.String() < b.ip.String()
	})
	result := []string{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		address := candidate.ip.String()
		if !seen[address] {
			result = append(result, address)
			seen[address] = true
		}
	}
	return result
}
