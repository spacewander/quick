package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInvalidResolveAddrs(t *testing.T) {
	assert.NotNil(t, config.revolver.Set("127.0.0.1:www.test.com"))
	assert.NotNil(t, config.revolver.Set("127.0.0.1:xxx:www.test.com"))
	assert.NotNil(t, config.revolver.Set("127.0.0.1:0:www.test.com"))
	assert.NotNil(t, config.revolver.Set(""))
	assert.NotNil(t, config.revolver.Set("[::1:80:[::1"))
	assert.NotNil(t, config.revolver.Set("[::1:80:[::1]"))
	assert.NotNil(t, config.revolver.Set("[::1]:80:[::1"))
	config.revolver.Set("www.test.com:443:127.0.0.1")
	config.revolver.Set("www.test.com:443:127.0.0.1:8443")
	assert.Equal(t, "www.test.com:443:127.0.0.1:8443 www.test.com:443:127.0.0.1:443",
		config.revolver.String())

	config.revolver = resolveValue{}
	config.revolver.Set("[::1]:443:[::2]:4445")
	config.revolver.Set("[::2]:445:[::1]")
	assert.Equal(t, "[::2]:445:[::1]:445 [::1]:443:[::2]:4445",
		config.revolver.String())
}

func assertCheckResolver(t *testing.T, args []string, expected string) {
	defer resetArgs()
	os.Args = append([]string{"cmd", "-resolve"}, args...)
	err := checkArgs()
	if err != nil {
		assert.Equal(t, expected, err.Error())
	} else {
		assert.Equal(t, expected, config.address)
	}
}

func TestResolve(t *testing.T) {
	assertCheckResolver(t, []string{"test.com:443:127.0.0.1", "test.com"},
		"https://127.0.0.1:443")
	assertCheckResolver(t, []string{"test.com:443:127.0.0.1", "www.test.com"},
		"https://www.test.com:443")
	assertCheckResolver(t, []string{"test.com:8443:127.0.0.1", "test.com"},
		"https://test.com:443")
	assertCheckResolver(t, []string{"test.com:8443:127.0.0.1:4443",
		"-resolve", "test.com:8443:127.0.0.1", "test.com:8443"},
		"https://127.0.0.1:8443")
}
