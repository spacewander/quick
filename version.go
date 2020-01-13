package main

// copied from quic-go/internal/protocol/version.go
// we should get rid of it once the HTTP3 is no longer a draft

import (
	"fmt"
	"math"
)

// VersionNumber is a version number as int
type VersionNumber uint32

// gQUIC version range as defined in the wiki: https://github.com/quicwg/base-drafts/wiki/QUIC-Versions
const (
	gquicVersion0   = 0x51303030
	maxGquicVersion = 0x51303439
)

// The version numbers, making grepping easier
const (
	VersionTLS      VersionNumber = VersionMilestone0_13
	VersionWhatever VersionNumber = 1 // for when the version doesn't matter
	VersionUnknown  VersionNumber = math.MaxUint32

	VersionMilestone0_13 VersionNumber = 0xff000017 // QUIC WG draft-23
)

// SupportedVersions lists the versions that the server supports
// must be in sorted descending order
var SupportedVersions = []VersionNumber{VersionMilestone0_13}

// IsValidVersion says if the version is known to quic-go
func IsValidVersion(v VersionNumber) bool {
	return v == VersionTLS || IsSupportedVersion(SupportedVersions, v)
}

func (vn VersionNumber) String() string {
	switch vn {
	case VersionWhatever:
		return "whatever"
	case VersionUnknown:
		return "unknown"
	case VersionMilestone0_13:
		return "QUIC WG draft-23"
	default:
		if vn.isGQUIC() {
			return fmt.Sprintf("gQUIC %d", vn.toGQUICVersion())
		}
		return fmt.Sprintf("%#x", uint32(vn))
	}
}

func (vn VersionNumber) isGQUIC() bool {
	return vn > gquicVersion0 && vn <= maxGquicVersion
}

func (vn VersionNumber) toGQUICVersion() int {
	return int(10*(vn-gquicVersion0)/0x100) + int(vn%0x10)
}

// IsSupportedVersion returns true if the server supports this version
func IsSupportedVersion(supported []VersionNumber, v VersionNumber) bool {
	for _, t := range supported {
		if t == v {
			return true
		}
	}
	return false
}
