package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

const (
	addrNotListened = "https://127.0.0.1:11111"
)

type ClientSuite struct {
	suite.Suite
}

func (suite *ClientSuite) SetupTest() {
	saveArgs()
}

func TestClientTestSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

func (suite *ClientSuite) TearDownTest() {
	resetArgs()
}

func (suite *ClientSuite) TestConnectTimeout() {
	address = addrNotListened
	connectTimeout = 10 * time.Millisecond

	t := suite.T()
	err := run()
	assert.NotNil(t, err)
	assert.Equal(t, "Get "+address+": connect timeout", err.Error())
}
