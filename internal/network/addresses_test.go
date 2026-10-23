package network

import (
	"net"
	"reflect"
	"testing"
)

func TestAddressOrderUsesInterfacesNotSubnetNames(t *testing.T) {
	candidates := []candidate{
		{ip: net.ParseIP("192.168.56.1"), index: 3},
		{ip: net.ParseIP("100.64.0.1"), index: 2, preference: preference{gateway: true, metric: 1}},
		{ip: net.ParseIP("10.20.30.40"), index: 1, preference: preference{gateway: true, physical: true, metric: 20}},
		{ip: net.ParseIP("10.20.30.40"), index: 1, preference: preference{gateway: true, physical: true, metric: 20}},
	}
	want := []string{"10.20.30.40", "100.64.0.1", "192.168.56.1"}
	if got := ordered(candidates); !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
}
