module github.com/spacewander/quick

go 1.13

require (
	// need to use the same protocol version with Caddy
	// TODO: find a reasonable way to adapt the protocol change since not everyone
	// is using Caddy based QUIC server.
	github.com/lucas-clemente/quic-go v0.13.1
	github.com/stretchr/testify v1.3.0
	github.com/zoidbergwill/hdrhistogram v0.0.0-20190826083824-4d99d8ade09d
	golang.org/x/net v0.0.0-20190404232315-eb5bcb51f2a3
)
