package webtor

import (
	"fmt"
	"strings"
)

// Capabilities is a bitset of API surfaces a backend supports. The resource
// surface (add/get/list/export) is universal and has no bit — every backend
// serves it.
type Capabilities uint32

const (
	// CapLibrary: the account library (/library).
	CapLibrary Capabilities = 1 << iota
	// CapVault: long-term storage pledges (/vault).
	CapVault
	// CapProfile: account profile (/profile).
	CapProfile
	// CapDeviceFlow: device authorization (/device/code, /device/token).
	CapDeviceFlow
)

func (c Capabilities) has(cap Capabilities) bool { return c&cap != 0 }

func (c Capabilities) String() string {
	var names []string
	for _, e := range []struct {
		cap  Capabilities
		name string
	}{
		{CapLibrary, "library"},
		{CapVault, "vault"},
		{CapProfile, "profile"},
		{CapDeviceFlow, "device-flow"},
	} {
		if c.has(e.cap) {
			names = append(names, e.name)
		}
	}
	return strings.Join(names, ",")
}

// CapabilityError is returned — before any HTTP round-trip — when a method
// needs a capability the configured backend does not have.
type CapabilityError struct {
	Backend    BackendKind
	Capability Capabilities
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("webtor: %s is not supported by the %s backend", e.Capability, e.Backend)
}
