package main

// this file contains tests which are guaranteed to pass by quic-go

import (
	"bytes"
	"io/ioutil"
	"net/http"
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

func TestSanitySuite(t *testing.T) {
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

func (suite *SanitySuite) TestOverrideContentType() {
	config.customHeaders.Set("Content-Type: text/plain")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Method + " " + r.Header.Get("Content-Type") + " "))
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		}
		w.Write(body)
	})
	done := startServer(handler)

	config.data.Set("hello world")
	config.method = http.MethodPost
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "POST text/plain hello world", string(b.Bytes()))
	}
	<-done
}
