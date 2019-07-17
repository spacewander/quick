package main

// this file contains tests which are guaranteed to pass by quic-go

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type SanitySuite struct {
	suite.Suite
}

func (suite *SanitySuite) SetupTest() {
	config.address = addrListened
	config.insecure = true
}

func TestSanityTestSuite(t *testing.T) {
	suite.Run(t, new(SanitySuite))
}

func (suite *SanitySuite) TearDownTest() {
	resetArgs()
}

func (suite *SanitySuite) TestInjectViaHeader() {
	config.customHeaders.Set("bad: header\r\nnew: header")

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	if err != nil {
		assert.True(t, strings.Contains(err.Error(),
			"invalid http header field"))
	} else {
		assert.Fail(t, "should fail")
	}
}
