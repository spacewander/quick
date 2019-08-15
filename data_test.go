package main

import (
	"io/ioutil"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithData(t *testing.T) {
	defer resetArgs()

	os.Args = []string{"cmd", "-d", "blah blah blah", "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	dataSrc, _ := config.data.Open("")
	data, _ := ioutil.ReadAll(dataSrc)
	assert.Equal(t, "blah blah blah", string(data))
	assert.Equal(t, http.MethodPost, config.method)
}

func TestInvalidData(t *testing.T) {
	assert.Equal(t, "empty data not allowed", config.data.Set("").Error())
	assert.Equal(t, "empty file name not allowed", config.data.Set("@").Error())
}

func assertCheckData(t *testing.T, args []string, expected string,
	contentType string) {
	defer resetArgs()

	os.Args = append([]string{"cmd"}, args...)
	os.Args = append(os.Args, "test.com")
	err := checkArgs()
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		dataSrc, err := config.data.Open(contentType)
		if err != nil {
			assert.Equal(t, expected, err.Error())
			return
		}
		defer dataSrc.Close()
		data, _ := ioutil.ReadAll(dataSrc)
		assert.Equal(t, expected, string(data))
	}
}

func TestReadData(t *testing.T) {
	assertCheckData(t, []string{"-d", "a", "-d", "b"}, "ab", "")
	assertCheckData(t, []string{"-d", "a", "-d", "b"}, "a&b",
		"application/x-www-form-urlencoded")

	_, fn := createTmpFile("c=d")
	defer os.Remove(fn)
	assertCheckData(t, []string{"-d", "a=b", "-d", "@" + fn, "-d", "e=f"},
		"a=b&c=d&e=f", "application/x-www-form-urlencoded")

	_, fn1 := createTmpFile("he")
	_, fn2 := createTmpFile("wor")
	defer os.Remove(fn1)
	defer os.Remove(fn2)
	assertCheckData(t, []string{"-d", "@" + fn1, "-d", "llo ", "-d", "@" + fn2, "-d", "ld"},
		"hello world", "text/plain")

	assertCheckData(t, []string{"-d", "@" + fn1, "-d", "llo ", "-d", "@non-exist", "-d", "ld"},
		"open non-exist: no such file or directory", "text/plain")
}
