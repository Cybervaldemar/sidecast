package network

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func interfacePreferences() map[int]preference {
	result := map[int]preference{}
	size := uint32(15000)
	for attempt := 0; attempt < 3; attempt++ {
		buffer := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		flags := uint32(windows.GAA_FLAG_INCLUDE_GATEWAYS | windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_DNS_SERVER)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if err != nil {
			return result
		}
		for adapter := first; adapter != nil; adapter = adapter.Next {
			value := preference{
				gateway:  adapter.FirstGatewayAddress != nil,
				physical: adapter.IfType == windows.IF_TYPE_ETHERNET_CSMACD || adapter.IfType == windows.IF_TYPE_IEEE80211,
				metric:   adapter.Ipv4Metric,
			}
			result[int(adapter.IfIndex)] = value
			if adapter.Ipv6IfIndex != 0 {
				result[int(adapter.Ipv6IfIndex)] = value
			}
		}
		break
	}
	return result
}
