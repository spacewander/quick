package main

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	oldArgs           []string
	oldMaxTime        time.Duration
	oldConnectTimeout time.Duration
	oldIdleTimeout    time.Duration

	oldHeadersOnly     bool
	oldHeadersIncluded bool
)

func saveArgs() {
	oldArgs = os.Args
	oldMaxTime = maxTime
	oldConnectTimeout = connectTimeout
	oldIdleTimeout = idleTimeout
	oldHeadersOnly = headersOnly
	oldHeadersIncluded = headersIncluded
}

func resetArgs() {
	os.Args = oldArgs
	maxTime = oldMaxTime
	connectTimeout = oldConnectTimeout
	idleTimeout = oldIdleTimeout
	headersOnly = oldHeadersOnly
	headersIncluded = oldHeadersIncluded

	customHeaders.hdr = http.Header{}
}

func assertCheckArgs(t *testing.T, args []string, expectedErrMsg string) {
	saveArgs()
	defer resetArgs()

	os.Args = append([]string{"cmd"}, args...)
	if expectedErrMsg == "" {
		assert.Equal(t, nil, checkArgs())
	} else {
		assert.Equal(t, expectedErrMsg, checkArgs().Error())
	}
}

func TestCheckArgs(t *testing.T) {
	assertCheckArgs(t, []string{"127.0.0.1:8443"}, "")
	assertCheckArgs(t, []string{"test.com"}, "")
	assertCheckArgs(t, []string{"/test"}, "URL invalid")
	assertCheckArgs(t, []string{"http://test.com"}, "URL invalid")
	assertCheckArgs(t, []string{"https://test.com"}, "")
	assertCheckArgs(t, []string{"quic://test.com"}, "URL invalid")
	assertCheckArgs(t, []string{}, "no URL specified")
	assertCheckArgs(t, []string{"-max-time", "-1s", "test.com"},
		"invalid argument: -max-time should not be negative, got -1s")
	assertCheckArgs(t, []string{"-connect-timeout", "-1s", "test.com"},
		"invalid argument: -connect-timeout should be positive, got -1s")
	assertCheckArgs(t, []string{"-connect-timeout", "1s",
		"-max-time", "100ms", "test.com"},
		"invalid argument: -max-time should be larger than other timeouts")
	assertCheckArgs(t, []string{"-idle-timeout", "1s",
		"-max-time", "100ms", "test.com"},
		"invalid argument: -max-time should be larger than other timeouts")
}

func assertCheckSNI(t *testing.T, args []string, expected string) {
	defer func() { sni = "" }()
	sni = ""
	assertCheckArgs(t, args, "")
	assert.Equal(t, expected, sni)
}

func TestCheckSNI(t *testing.T) {
	// use IP address as SNI is invalid
	assertCheckSNI(t, []string{"test.com"}, "test.com")
	assertCheckSNI(t, []string{"test.com:8443"}, "test.com")
	assertCheckSNI(t, []string{"-sni", "hi", "127.0.0.1:8443"}, "hi")
	assertCheckSNI(t, []string{"-sni", "hi:123", "127.0.0.1:8443"}, "hi")
}

func assertCheckAddr(t *testing.T, args []string, expected string) {
	defer func() { address = "" }()
	address = ""
	assertCheckArgs(t, args, "")
	assert.Equal(t, expected, address)
}

func TestCheckAddr(t *testing.T) {
	assertCheckAddr(t, []string{"test.com"}, "https://test.com:443")
	assertCheckAddr(t, []string{"127.0.0.1"}, "https://127.0.0.1:443")
	assertCheckAddr(t, []string{"127.0.0.1:8000"}, "https://127.0.0.1:8000")
	assertCheckAddr(t, []string{"https://test.com"}, "https://test.com:443")
	assertCheckAddr(t, []string{"https://127.0.0.1"}, "https://127.0.0.1:443")
	assertCheckAddr(t, []string{"https://127.0.0.1:8443"}, "https://127.0.0.1:8443")
}

func assertCheckHeaders(t *testing.T, args []string, expected string) {
	saveArgs()
	defer resetArgs()

	os.Args = append([]string{"cmd"}, args...)
	os.Args = append(os.Args, "test.com")
	err := checkArgs()
	if err == nil {
		if expected == "" {
			assert.Fail(t, "should fail")
		} else {
			assert.Equal(t, expected, customHeaders.String())
		}
	} else {
		if expected != "" {
			assert.NotNil(t, err, err.Error())
		}
	}
}

func TestCheckHeaders(t *testing.T) {
	assertCheckHeaders(t, []string{"-H", "xx : yy"}, "Xx: yy")
	assertCheckHeaders(t, []string{"-H", "x_x : yy "}, "X_x: yy")
	assertCheckHeaders(t, []string{"-H", " x_x:yy"}, "X_x: yy")
	assertCheckHeaders(t, []string{"-H", "A: B", "-H", "B: C", "-H", "A: C"}, "A: B\r\nA: C\r\nB: C")
}

func TestInvalidHeader(t *testing.T) {
	assert.NotNil(t, customHeaders.Set("A"))
	assert.NotNil(t, customHeaders.Set("A:"))
	assert.NotNil(t, customHeaders.Set(":A"))
	assert.NotNil(t, customHeaders.Set(" : "))
}
