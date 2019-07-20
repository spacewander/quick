module github.com/spacewander/quick

go 1.12

require (
	// need to use the same protocol version with Caddy
	// TODO: find a reasonable way to adapt the protocol change since not everyone
	// is using Caddy based QUIC server.
	github.com/lucas-clemente/quic-go v0.10.2

	// dev dependencies
	github.com/stretchr/testify v1.3.0
	golang.org/x/net v0.0.0-20180906233101-161cd47e91fd
)
