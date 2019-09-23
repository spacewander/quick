package main

// copied from quic-go/internal/protocol/version.go

import (
	"fmt"
)

// VersionNumber is a version number as int
type VersionNumber uint32

// gQUIC version range as defined in the wiki: https://github.com/quicwg/base-drafts/wiki/QUIC-Versions
const (
	gquicVersion0 = 0x51303030
)

// The version numbers, making grepping easier
const (
	Version39 VersionNumber = gquicVersion0 + 3*0x100 + 0x9
	Version43 VersionNumber = gquicVersion0 + 4*0x100 + 0x3
	Version44 VersionNumber = gquicVersion0 + 4*0x100 + 0x4
)

// SupportedVersions lists the versions that the server supports
// must be in sorted descending order
var SupportedVersions = []VersionNumber{
	Version44,
	Version43,
	Version39,
}

func (vn VersionNumber) String() string {
	return fmt.Sprintf("gQUIC %d", vn.toGQUICVersion())
}

func (vn VersionNumber) toGQUICVersion() int {
	return int(10*(vn-gquicVersion0)/0x100) + int(vn%0x10)
}
