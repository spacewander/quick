package main

import (
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFormArg(t *testing.T) {
	fv := formValue{}
	assert.Nil(t, fv.Set("web=@index.html"))
	assert.Equal(t, "name=web;filename=index.html;data=index.html", fv.String())
	assert.Nil(t, fv.Set("name=daniel;  type=text/foo"))
	assert.Equal(t, `name=web;filename=index.html;data=index.html
name=name;type=text/foo;data=daniel`, fv.String())
	assert.Nil(t, fv.Set(`web="@localhost"; filename="it's fine.png"`))
	assert.Equal(t, "name=web;filename=it's fine.png;data=localhost", fv.lastForm().String())
	assert.Nil(t, fv.Set(`colors="red; green; blue";xx=yy; type=text/x-myapp`))
	assert.Equal(t, "name=colors;type=text/x-myapp;data=red; green; blue", fv.lastForm().String())
	assert.Nil(t, fv.Set(`name=data"`))
	assert.Equal(t, "name=name;data=data\"", fv.lastForm().String())
	assert.Nil(t, fv.Set("web=@path/to/index.html"))
	assert.Equal(t, "name=web;filename=index.html;data=path/to/index.html", fv.lastForm().String())

	assert.Nil(t, fv.Set(`name=`))
	assert.Equal(t, "name=name;data=", fv.lastForm().String())
	assert.Nil(t, fv.Set(`name=""`))

	assert.NotNil(t, fv.Set(`=y`))
	assert.NotNil(t, fv.Set(`name=@`))
}

func TestFormConflictsWithData(t *testing.T) {
	defer resetArgs()
	os.Args = []string{"cmd", "-F", "name=x", "-d", "xx", "test.com"}
	err := checkArgs()
	assert.Equal(t, "invalid argument: -d can't be used with -F", err.Error())
}

func TestWithForm(t *testing.T) {
	defer resetArgs()

	os.Args = []string{"cmd", "-F", `name="b\"c"; filename="\"a\""`, "test.com"}
	err := checkArgs()
	assert.Nil(t, err)
	assert.Equal(t, http.MethodPost, config.method)
	assert.Equal(t, `name=name;filename="a";data=b"c`, config.forms.String())
}
