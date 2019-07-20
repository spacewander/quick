package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func mustNewCookieManager() *cookieManager {
	cm, err := newCookieManager()
	if err != nil {
		panic(err)
	}
	return cm.(*cookieManager)
}

func TestLoadCookie(t *testing.T) {
	cm := mustNewCookieManager()
	err := cm.Load("testdata/cookies.txt")
	if err != nil {
		assert.Fail(t, err.Error())
		return
	}

	out := `www.taobao.com	TRUE	/xxx	FALSE	1594549400	pro	gd
taobao.com	TRUE	/	FALSE	1594549396	thw	cn
taobao.com	TRUE	/	FALSE	1594549397	id
taobao.com	TRUE	/	TRUE	1594549398	sid	12345
www.taobao.com	TRUE	/	TRUE	1594549399	cid`
	assert.Equal(t, out, cm.String())
}

func TestLoadCookieFieldsNotEnough(t *testing.T) {
	cm := mustNewCookieManager()
	err := cm.Load("testdata/malformed_cookies1.txt")
	if err == nil {
		assert.Fail(t, "should fail")
	} else {
		assert.Equal(t, "invalid cookie entry(.taobao.com\tTRUE\t/\tFALSE\t1594549396): not enough fields", err.Error())
	}
}

func TestLoadCookieInvalidSecure(t *testing.T) {
	cm := mustNewCookieManager()
	err := cm.Load("testdata/malformed_cookies2.txt")
	if err == nil {
		assert.Fail(t, "should fail")
	} else {
		assert.Equal(t, "invalid cookie entry(.www.taobao.com\tTRUE\t/\txx\t1594549399\tcid): unrecognized secure", err.Error())
	}
}

func TestLoadCookieInvalidExpiration(t *testing.T) {
	cm := mustNewCookieManager()
	err := cm.Load("testdata/malformed_cookies3.txt")
	if err == nil {
		assert.Fail(t, "should fail")
	} else {
		assert.Equal(t, "invalid cookie entry(www.taobao.com\tTRUE\t/xxx\tFALSE\txxx\tpro\tgd): strconv.Atoi: parsing \"xxx\": invalid syntax", err.Error())
	}
}

func TestLoadCookieFromStr(t *testing.T) {
	cm := mustNewCookieManager()
	err := cm.LoadCookiesForURL("https://127.0.0.1", "name=value; secure=true")
	if err != nil {
		assert.Fail(t, err.Error())
		return
	}

	assert.Equal(t, "127.0.0.1\tTRUE\t/\tTRUE\t253402300799\tname\tvalue",
		cm.String())
}

func TestLoadCookieFromInvalidStr(t *testing.T) {
	cm := mustNewCookieManager()
	err := cm.LoadCookiesForURL("https://127.0.0.1", "name;xx")
	if err != nil {
		assert.Equal(t, "invalid cookies string", err.Error())
		return
	}

	assert.NotNil(t, err)
}

func TestLoadCookieFileNotExists(t *testing.T) {
	cm := mustNewCookieManager()
	err := cm.Load("non-exist")
	if err != nil {
		assert.Equal(t, "open non-exist: no such file or directory", err.Error())
		return
	}
	assert.NotNil(t, err)
}

func TestDumpCookieFileNotExists(t *testing.T) {
	cm := mustNewCookieManager()
	err := cm.Dump("/non/exist")
	if err != nil {
		assert.Equal(t, "open /non/exist: no such file or directory", err.Error())
		return
	}
	assert.NotNil(t, err)
}

func TestDumpCookie(t *testing.T) {
	cm := mustNewCookieManager()
	cm.Load("testdata/cookies.txt")
	expect := cm.String()
	_, fn := createTmpFile("")
	defer os.Remove(fn)
	cm.Dump(fn)

	cm = mustNewCookieManager()
	cm.Load(fn)
	actual := cm.String()
	assert.Equal(t, expect, actual)
}
