package discovery

import (
	"net"
	"testing"
)

func TestPhoneURLSupportsIPv6(t *testing.T) {
	phone := &PhoneInfo{IP: net.ParseIP("2001:db8::1"), Port: 8384}
	if got, want := phoneURL(phone, "/api/media/list"), "http://[2001:db8::1]:8384/api/media/list"; got != want {
		t.Fatalf("phoneURL = %q, want %q", got, want)
	}
}
