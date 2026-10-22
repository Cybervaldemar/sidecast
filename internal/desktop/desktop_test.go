package desktop

import "testing"

func TestLocalURL(t *testing.T) {
	for _, test := range []struct {
		address string
		secure  bool
		want    string
	}{
		{"[::]:8080", false, "http://localhost:8080"},
		{"0.0.0.0:9443", true, "https://localhost:9443"},
		{"127.0.0.1:8081", false, "http://127.0.0.1:8081"},
		{"[::1]:8443", true, "https://[::1]:8443"},
		{"192.168.1.20:8080", false, "http://192.168.1.20:8080"},
	} {
		if got := LocalURL(test.address, test.secure); got != test.want {
			t.Errorf("LocalURL(%q, %v) = %q, want %q", test.address, test.secure, got, test.want)
		}
	}
}
