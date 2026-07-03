module github.com/spacewander/quick

go 1.25.0

require (
	// need to use the same protocol version with Caddy
	// TODO: find a reasonable way to adapt the protocol change since not everyone
	// is using Caddy based QUIC server.
	github.com/lucas-clemente/quic-go v0.10.2
	github.com/stretchr/testify v1.3.0
	github.com/zoidbergwill/hdrhistogram v0.0.0-20190826083824-4d99d8ade09d
	golang.org/x/net v0.55.0
)

require (
	github.com/bifurcation/mint v0.0.0-20180715133206-93c51c6ce115 // indirect
	github.com/cheekybits/genny v0.0.0-20170328200008-9127e812e1e9 // indirect
	github.com/davecgh/go-spew v1.1.0 // indirect
	github.com/hashicorp/golang-lru v0.0.0-20180201235237-0fb14efe8c47 // indirect
	github.com/lucas-clemente/aes12 v0.0.0-20171027163421-cd47fb39b79f // indirect
	github.com/lucas-clemente/quic-go-certificates v0.0.0-20160823095156-d2f86524cced // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
