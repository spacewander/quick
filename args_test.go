package main

// this file contains tests which are relative with arguments check

import (
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func resetArgs() {
	newCfg := newQuickConfig()

	aStruct := reflect.ValueOf(config).Elem()
	bStruct := reflect.ValueOf(newCfg).Elem()
	// need to copy field by field, because the address of each field is
	// referred by the flag.Var.
	for i := 0; i < aStruct.NumField(); i++ {
		aField := aStruct.Field(i)
		aField = reflect.NewAt(aField.Type(),
			unsafe.Pointer(aField.UnsafeAddr())).Elem()

		bField := bStruct.Field(i)
		bField = reflect.NewAt(bField.Type(),
			unsafe.Pointer(bField.UnsafeAddr())).Elem()

		aField.Set(bField)
	}
}

func assertCheckArgs(t *testing.T, args []string, expectedErrMsg string) {
	defer resetArgs()

	os.Args = append([]string{"cmd"}, args...)
	err := checkArgs()
	if expectedErrMsg == "" {
		assert.Equal(t, nil, err)
	} else {
		if err == nil {
			assert.Fail(t, "should fail")
		} else {
			assert.Equal(t, expectedErrMsg, err.Error())
		}
	}
}

func TestCheckArgs(t *testing.T) {
	assertCheckArgs(t, []string{"127.0.0.1:8443"}, "")
	assertCheckArgs(t, []string{"test.com"}, "")
	assertCheckArgs(t, []string{"/test"}, "URL invalid")
	assertCheckArgs(t, []string{"http://test.com"}, "URL invalid")
	assertCheckArgs(t, []string{"https://test.com"}, "")
	assertCheckArgs(t, []string{"%@3"},
		"parse https://%@3: invalid URL escape \"%\"")
	assertCheckArgs(t, []string{"quic://test.com"}, "URL invalid")
	assertCheckArgs(t, []string{}, "no URL specified")

	assertCheckArgs(t, []string{"-max-time", "-1s", "test.com"},
		"invalid argument: -max-time should not be negative, got -1s")
	assertCheckArgs(t, []string{"-connect-timeout", "-1s", "test.com"},
		"invalid argument: -connect-timeout should be positive, got -1s")
	assertCheckArgs(t, []string{"-idle-timeout", "-1s", "test.com"},
		"invalid argument: -idle-timeout should not be negative, got -1s")

	assertCheckArgs(t, []string{"-X", "get", "test.com"}, "")
	assertCheckArgs(t, []string{"-X", "Get", "test.com"}, "")
	assertCheckArgs(t, []string{"-X", "connect", "test.com"},
		"invalid argument: method CONNECT is unsupported")
	assertCheckArgs(t, []string{"-X", "xxx", "test.com"},
		"invalid argument: unknown method XXX")

	assertCheckArgs(t, []string{"-cookie", "xx=yy", "-load-cookie", "x.txt", "test.com"},
		"invalid argument: -cookie can't be used with -load-cookie")
}

func TestCheckMaxTime(t *testing.T) {
	os.Args = []string{"cmd", "-connect-timeout", "1s", "-max-time", "100ms", "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	assert.Equal(t, 100*time.Millisecond, config.maxTime)
	assert.Equal(t, 100*time.Millisecond, config.connectTimeout)

	os.Args = []string{"cmd", "-idle-timeout", "1s", "-max-time", "10ms", "test.com"}
	err = checkArgs()
	assert.Nil(t, err)
	assert.Equal(t, 10*time.Millisecond, config.idleTimeout)
}

func assertCheckSNI(t *testing.T, args []string, expected string) {
	defer resetArgs()

	os.Args = append([]string{"cmd"}, args...)
	err := checkArgs()
	if err == nil {
		if expected == "" {
			assert.Fail(t, "should fail")
		} else {
			assert.Equal(t, expected, config.sni)
		}
	} else {
		if expected != "" {
			assert.NotNil(t, err, err.Error())
		}
	}
}

func TestCheckSNI(t *testing.T) {
	// use IP address as SNI is invalid
	assertCheckSNI(t, []string{"test.com"}, "test.com")
	assertCheckSNI(t, []string{"test.com:8443"}, "test.com")
	assertCheckSNI(t, []string{"-sni", "hi", "127.0.0.1:8443"}, "hi")
	assertCheckSNI(t, []string{"-sni", "hi:123", "127.0.0.1:8443"}, "hi")

	assertCheckArgs(t, []string{"-sni", "hi:1:123", "127.0.0.1:8443"},
		"address hi:1:123: too many colons in address")
}

func assertCheckAddr(t *testing.T, args []string, expected string) {
	defer resetArgs()

	os.Args = append([]string{"cmd"}, args...)
	err := checkArgs()
	if err == nil {
		if expected == "" {
			assert.Fail(t, "should fail")
		} else {
			assert.Equal(t, expected, config.address)
		}
	} else {
		if expected != "" {
			assert.NotNil(t, err, err.Error())
		}
	}
}

func TestCheckAddr(t *testing.T) {
	assertCheckAddr(t, []string{"test.com"}, "https://test.com:443")
	assertCheckAddr(t, []string{"127.0.0.1"}, "https://127.0.0.1:443")
	assertCheckAddr(t, []string{"127.0.0.1:8000"}, "https://127.0.0.1:8000")
	assertCheckAddr(t, []string{"https://test.com"}, "https://test.com:443")
	assertCheckAddr(t, []string{"https://127.0.0.1"}, "https://127.0.0.1:443")
	assertCheckAddr(t, []string{"https://127.0.0.1:8443"},
		"https://127.0.0.1:8443")
	assertCheckAddr(t, []string{"127.0.0.1:8000/xxx?a=2"},
		"https://127.0.0.1:8000/xxx?a=2")
}

func assertCheckHeaders(t *testing.T, args []string, expected string) {
	defer resetArgs()

	os.Args = append([]string{"cmd"}, args...)
	os.Args = append(os.Args, "test.com")
	err := checkArgs()
	if err == nil {
		if expected == "" {
			assert.Fail(t, "should fail")
		} else {
			assert.Equal(t, expected, config.customHeaders.String())
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
	assertCheckHeaders(t, []string{"-H", "A: B", "-H", "B: C", "-H", "A: C"},
		"A: B\r\nA: C\r\nB: C")
}

func TestInvalidHeader(t *testing.T) {
	assert.NotNil(t, config.customHeaders.Set("A"))
	assert.NotNil(t, config.customHeaders.Set("A:"))
	assert.NotNil(t, config.customHeaders.Set(":A"))
	assert.NotNil(t, config.customHeaders.Set(" : "))
}

func TestEnsureMethodIsUpper(t *testing.T) {
	defer resetArgs()

	os.Args = []string{"cmd", "-X", "head", "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	assert.Equal(t, http.MethodHead, config.method)
}

func TestNoRedirct(t *testing.T) {
	defer resetArgs()

	os.Args = []string{"cmd", "-no-redirect", "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	assert.True(t, config.noRedirect)
}

func TestSetOutputFile(t *testing.T) {
	defer resetArgs()
	os.Args = []string{"cmd", "-o", "xxx", "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	assert.Equal(t, "xxx", config.outFilename)
}

func TestSetContentType(t *testing.T) {
	defer resetArgs()
	os.Args = []string{"cmd", "-H", "Content-Type: xxx", "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	assert.Equal(t, "xxx", config.contentType)
}

func TestHeadersOnly(t *testing.T) {
	defer resetArgs()

	os.Args = []string{"cmd", "-I", "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	assert.True(t, config.headersOnly)
	assert.Equal(t, http.MethodHead, config.method)
}

func TestEnableBenchmakrMode(t *testing.T) {
	defer resetArgs()
	os.Args = []string{"cmd", "-bm-duration", "1s", "-bm-req-per-conn", "3", "-bm-conn", "12", "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	assert.True(t, config.bmEnabled)
	assert.True(t, config.noRedirect)
	assert.True(t, config.insecure)
}

func TestCheckArgsWhenBenchmarkModeEnaled(t *testing.T) {
	bmEnabledArgs := []string{"-bm-duration", "1s", "-bm-req-per-conn", "3", "-bm-conn", "12", "test.com"}
	assertCheckArgs(t, append([]string{"-I"}, bmEnabledArgs...),
		"output customization is not allowed in benchmark mode")
	assertCheckArgs(t, append([]string{"-dump-cookie", "x.txt"}, bmEnabledArgs...),
		"unsupport option in benchmark mode")
}
