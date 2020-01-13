package main

// this file contains tests which run through between client and server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var (
	builtWithRace = flag.Bool("race", false, "built with race detector")
)

const (
	addrNotListened = "https://127.0.0.1:11111"
)

type ClientSuite struct {
	suite.Suite
}

func (suite *ClientSuite) SetupTest() {
	config.address = addrListened
	config.insecure = true
}

func TestClientSuite(t *testing.T) {
	flag.Parse()
	suite.Run(t, new(ClientSuite))
}

func (suite *ClientSuite) TearDownTest() {
	resetArgs()
}

func (suite *ClientSuite) TestConnectTimeout() {
	config.address = addrNotListened
	config.connectTimeout = 10 * time.Millisecond

	t := suite.T()
	err := run(&bytes.Buffer{})
	assert.NotNil(t, err)
	assert.Equal(t, "Get "+config.address+": connect timeout",
		err.Error())
}

func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template,
		&template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key)},
	)
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{tlsCert}}
}

func (suite *ClientSuite) TestMaxTime() {
	config.connectTimeout = 90 * time.Millisecond
	config.maxTime = 100 * time.Millisecond

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 1000; i++ {
			time.Sleep(1 * time.Millisecond)
		}
	})
	done := startServer(handler)

	t := suite.T()
	err := run(&bytes.Buffer{})
	done <- struct{}{}
	if err == nil {
		assert.NotNil(t, err)
	} else {
		assert.Equal(t,
			"Get "+config.address+": context deadline exceeded", err.Error())
	}
	<-done
}

func (suite *ClientSuite) TestMaxTimeReadBodyTimeout() {
	config.connectTimeout = 90 * time.Millisecond
	config.maxTime = 100 * time.Millisecond

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 1000; i++ {
			w.Write([]byte("a"))
			time.Sleep(1 * time.Millisecond)
		}
	})
	done := startServer(handler)

	t := suite.T()
	err := run(&bytes.Buffer{})
	done <- struct{}{}
	if err == nil {
		assert.NotNil(t, err)
	} else {
		assert.Equal(t,
			"failed to copy the output from "+config.address+": context deadline exceeded",
			err.Error())
	}
	<-done
}

func (suite *ClientSuite) TestIdleTimeout() {
	config.idleTimeout = 30 * time.Millisecond

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 1000; i++ {
			time.Sleep(1 * time.Millisecond)
		}
	})
	done := startServer(handler)

	t := suite.T()
	err := run(&bytes.Buffer{})
	done <- struct{}{}
	if err == nil {
		assert.NotNil(t, err)
	} else {
		assert.Equal(t,
			"Get "+config.address+": InvalidHeadersStreamData: NetworkIdleTimeout: No recent network activity.",
			err.Error())
	}
	<-done
}

func (suite *ClientSuite) TestIdleTimeoutWithDiscreteWrites() {
	config.idleTimeout = 30 * time.Millisecond

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 10; i++ {
			time.Sleep(10 * time.Millisecond)
			w.Write([]byte("1"))
		}
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.NotNil(t, err)
	} else {
		assert.Equal(t, bytes.Repeat([]byte("1"), 10), b.Bytes())
	}
	<-done
}

func (suite *ClientSuite) TestUserAgent() {
	config.userAgent = "opensema"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.UserAgent()))
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.Equal(t, config.userAgent, string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestDefaultUserAgent() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.UserAgent()))
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.Equal(t, "quick/"+version, string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestReadResponseBody() {
	data := bytes.Repeat([]byte("hello world"), 1024)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.Equal(t, string(data), string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestReadResponseHeaderInclude() {
	data := "hello world"
	body := bytes.Repeat([]byte("test"), 10)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("data", data)
		w.Write(body)
	})
	done := startServer(handler)

	config.headersIncluded = true

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.True(t, bytes.Contains(b.Bytes(), []byte("HTTP/2.0 200 OK\r\n")))
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Data: "+data+"\r\n")))
		assert.True(t, bytes.Contains(b.Bytes(), []byte("\r\n\r\n")))
		assert.True(t, bytes.Contains(b.Bytes(), body))
	}
	<-done
}

func (suite *ClientSuite) TestSendHeader() {
	config.customHeaders.Set(" input : blahblah ")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Write(w)
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Input: blahblah\r\n")))
	}
	<-done
}

func (suite *ClientSuite) TestSendHeaders() {
	config.customHeaders.Set(" input : blahblah ")
	config.customHeaders.Set(" this :  value ")
	config.customHeaders.Set(" this :  is ok")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Write(w)
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Input: blahblah\r\n")))
		assert.True(t, bytes.Contains(b.Bytes(), []byte("This: value\r\n")))
		assert.True(t, bytes.Contains(b.Bytes(), []byte("This: is ok\r\n")))
	}
	<-done
}

func (suite *ClientSuite) TestURLWithQueryString() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.RequestURI))
	})
	done := startServer(handler)

	config.address = addrListened + "/xxx?a=1&b=2"
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.Equal(t, "/xxx?a=1&b=2", string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestReadResponseBodyWithHEAD() {
	data := bytes.Repeat([]byte("hello world"), 1024)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	})
	done := startServer(handler)

	config.method = http.MethodHead
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.Equal(t, "", string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestReadResponseHeaderOnlyWithSpecificMethod() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("method", r.Method)
		w.Write(bytes.Repeat([]byte("test"), 1000))
	})
	done := startServer(handler)

	config.headersOnly = true
	config.method = http.MethodGet

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.True(t, bytes.Contains(b.Bytes(), []byte("HTTP/2.0 200 OK\r\n")))
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Method: GET\r\n")))
		assert.False(t, bytes.Contains(b.Bytes(), []byte("\r\n\r\n")))
		assert.False(t, bytes.Contains(b.Bytes(), []byte("test")))
	}
	<-done
}

func (suite *ClientSuite) TestReadResponseBodyWithDELETE() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Method + " " + r.RequestURI))
	})
	done := startServer(handler)

	config.method = http.MethodDelete
	config.address += "/users/1"
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.Equal(t, "DELETE /users/1", string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestEnableRedirectByDefault() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.RequestURI, "/redirect") {
			w.Write([]byte("done"))
		} else {
			http.Redirect(w, r, "/redirect", 302)
		}
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.Equal(t, "done", string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestDisableRedirect() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.RequestURI, "/redirect") {
			w.Write([]byte("done"))
		} else {
			http.Redirect(w, r, "/redirect", 302)
		}
	})
	done := startServer(handler)

	config.headersOnly = true
	config.noRedirect = true
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.True(t,
			strings.Contains(string(b.Bytes()), "Location: /redirect\r\n"))
	}
	<-done
}

func (suite *ClientSuite) TestPost() {
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
		assert.Equal(t, "POST "+defaultContentType+" hello world", string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestPostWithoutBody() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Method + " " + r.Header.Get("Content-Type")))
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		}
		w.Write(body)
	})
	done := startServer(handler)

	config.method = http.MethodPost
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "POST "+defaultContentType, string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestGetWithBody() {
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
	config.method = http.MethodGet
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "GET "+defaultContentType+" hello world", string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestOverrideHost() {
	config.customHeaders.Set("Host: www.test.com")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Host))
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "www.test.com", string(b.Bytes()))
	}
	<-done
}

func (suite *ClientSuite) TestCookieFromStr() {
	exp := time.Now().Add(120 * time.Second)
	var buf [len(http.TimeFormat)]byte
	expStr := string(exp.UTC().AppendFormat(buf[:0], http.TimeFormat))
	config.cookie = "name=value; Path=/xxx\n" +
		"name=value2; Path=/; Expires=" + expStr
	_, fn := createTmpFile("")
	defer os.Remove(fn)
	config.dumpCookie = fn

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:    "key",
			Expires: exp,
		})
		val, err := r.Cookie("name")
		if err == nil {
			w.Write([]byte(val.Value))
		}
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "value2", string(b.Bytes()))
	}
	<-done

	f, _ := os.Open(fn)
	defer f.Close()
	data, _ := ioutil.ReadAll(f)
	s := fmt.Sprintf("127.0.0.1\tTRUE\t/xxx\tFALSE\t253402300799\tname\tvalue\n"+
		"127.0.0.1\tTRUE\t/\tFALSE\t%d\tname\tvalue2\n"+
		"127.0.0.1\tTRUE\t/\tFALSE\t%d\tkey\t\n", exp.Unix(), exp.Unix())
	assert.Equal(t, s, string(data))
}

func (suite *ClientSuite) TestCookieFromFile() {
	exp := time.Now().Add(120 * time.Second)
	s := "127.0.0.1\tTRUE\t/\tFALSE\t2094549396\tname\tvalue2\n" +
		"www.test.com\tTRUE\t/\tFALSE\t2094549396\tname\t\n"
	_, fn := createTmpFile(s)
	defer os.Remove(fn)
	config.loadCookie = fn
	config.dumpCookie = fn

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:    "key",
			Value:   "value",
			Expires: exp,
			Domain:  "www.google.com",
		})
		val, err := r.Cookie("name")
		if err == nil {
			w.Write([]byte(val.Value))
		}
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "value2", string(b.Bytes()))
	}
	<-done

	f, _ := os.Open(fn)
	defer f.Close()
	data, _ := ioutil.ReadAll(f)
	assert.Equal(t, s, string(data))
}

func (suite *ClientSuite) TestMultipleSameHeaders() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("hdr", "first")
		w.Header().Add("hdr", "second")
	})
	done := startServer(handler)

	config.headersOnly = true

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Hdr: first\r\nHdr: second\r\n")))
	}
	<-done
}

func (suite *ClientSuite) TestWriteOutputToFile() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("hdr", "name")
		w.Write([]byte("abcde"))
	})
	done := startServer(handler)

	config.headersIncluded = true
	dir := createTmpDir()
	defer os.Remove(dir)
	fn := filepath.Join(dir, "TestWriteOutputToFile")
	config.outFilename = fn

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		f, _ := os.Open(fn)
		defer f.Close()
		data, _ := ioutil.ReadAll(f)
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Hdr: name\r\n")))
		assert.False(t, bytes.Contains(b.Bytes(), []byte("abcde")))
		assert.Equal(t, "abcde", string(data))
	}
	<-done
}

func (suite *ClientSuite) TestWriteOutputToFileWhileHeaderOnly() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("hdr", "name")
		w.Write([]byte("abcde"))
	})
	done := startServer(handler)

	config.headersOnly = true
	dir := createTmpDir()
	defer os.Remove(dir)
	fn := filepath.Join(dir, "TestWriteOutputToFileWhileHeaderOnly")
	config.outFilename = fn

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Hdr: name\r\n")))
		assert.False(t, bytes.Contains(b.Bytes(), []byte("abcde")))
		if _, err := os.Stat(fn); os.IsExist(err) {
			assert.Fail(t, "should not crerate output file")
		}
	}
	<-done
}

func (suite *ClientSuite) TestFailedToCreateOutputFile() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("abcde"))
	})
	done := startServer(handler)

	config.outFilename = "/x/y/z"

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Equal(t, "mkdir /x: permission denied", err.Error())
	} else {
		assert.Fail(t, "should fail")
	}
	<-done
}

func (suite *ClientSuite) TestFailedToCreateParentDirs() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("abcde"))
	})
	done := startServer(handler)

	dir := createTmpDir()
	defer os.Remove(dir)
	fn := filepath.Join(dir, "x", "y", "z")
	config.outFilename = fn

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		f, _ := os.Open(fn)
		defer f.Close()
		data, _ := ioutil.ReadAll(f)
		assert.Equal(t, "abcde", string(data))
	}
	<-done
}

func (suite *ClientSuite) TestResolveWithRedirect() {
	var lock sync.Mutex
	var originHostHdr string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.RequestURI, "/redirect2") {
			w.Write([]byte("done"))
		} else if strings.HasPrefix(r.RequestURI, "/redirect1") {
			http.Redirect(w, r, "https://test.com:5443/redirect2", 302)
		} else {
			lock.Lock()
			originHostHdr = r.Host
			lock.Unlock()
			http.Redirect(w, r, "https://www.test.com/redirect1", 302)
		}
	})
	done := startServer(handler)
	uri, _ := url.Parse(addrListened)
	host := uri.Host
	config.revolver.Set("www.test.com:443:" + host)
	config.revolver.Set("test.com:5443:" + host)
	config.revolver.Set("www.origin.com:443:" + host)
	resolveAddr("www.origin.com", config)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "done", string(b.Bytes()))
		lock.Lock()
		assert.Equal(t, "www.origin.com", originHostHdr)
		lock.Unlock()
	}
	<-done
}

func (suite *ClientSuite) TestResolveWithRedirect_TestReferer() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.RequestURI, "/redirect1") {
			w.Write([]byte(r.Referer()))
		} else {
			http.Redirect(w, r, "https://www.test.com/redirect1", 302)
		}
	})
	done := startServer(handler)
	uri, _ := url.Parse(addrListened)
	host := uri.Host
	config.revolver.Set("www.test.com:443:" + host)
	config.revolver.Set("www.origin.com:443:" + host)
	resolveAddr("www.origin.com:443", config)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "https://www.origin.com", string(b.Bytes()))
	}
	<-done
}

type partData struct {
	name     string
	filename string
	headers  textproto.MIMEHeader
	body     string
}

func newPartData(name, filename, contentType, body string) *partData {
	hdrs := textproto.MIMEHeader{}
	hdrs.Add("Content-Type", contentType)
	return &partData{name: name, filename: filename, body: body, headers: hdrs}
}

func newParDataFromPart(p *multipart.Part) *partData {
	pd := &partData{}
	pd.filename = p.FileName()
	pd.name = p.FormName()
	p.Header.Del("Content-Disposition")
	pd.headers = p.Header
	data, _ := ioutil.ReadAll(p)
	pd.body = string(data)
	return pd
}

func (suite *ClientSuite) TestPostMultipartForm() {
	var actual []*partData
	var lock sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		mt, params, _ := mime.ParseMediaType(ct)
		w.Write([]byte(r.Method + " " + mt))
		mr := multipart.NewReader(r.Body, params["boundary"])
		lock.Lock()
		defer lock.Unlock()
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				return
			}
			actual = append(actual, newParDataFromPart(p))
		}
	})
	done := startServer(handler)

	config.forms.Set(`colors="red;\" green";type=text/plain`)
	expected := []*partData{
		newPartData("colors", "", "text/plain", "red;\" green"),
	}
	config.method = http.MethodPost
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	lock.Lock()
	defer lock.Unlock()
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "POST multipart/form-data", string(b.Bytes()))
		assert.Equal(t, expected, actual)
	}
	<-done
}

func (suite *ClientSuite) TestPostMultipartFormFile() {
	var actual []*partData
	var lock sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		_, params, _ := mime.ParseMediaType(ct)
		mr := multipart.NewReader(r.Body, params["boundary"])
		lock.Lock()
		defer lock.Unlock()
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				return
			}
			actual = append(actual, newParDataFromPart(p))
		}
	})
	done := startServer(handler)

	f, _ := os.Open("testdata/cookies.txt")
	data, _ := ioutil.ReadAll(f)
	config.forms.Set(`name=@testdata/cookies.txt; filename=cookies; type=text/plain`)
	expected := []*partData{
		newPartData("name", "cookies", "text/plain", string(data)),
	}
	config.method = http.MethodPost
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	lock.Lock()
	defer lock.Unlock()
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, expected, actual)
	}
	<-done
}

func (suite *ClientSuite) TestPostMultipartFormFileNotExist() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		_, params, _ := mime.ParseMediaType(ct)
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				return
			}
			io.Copy(ioutil.Discard, p)
		}
	})
	done := startServer(handler)

	config.forms.Set(`name=@path/to/localhost`)
	config.method = http.MethodPost
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.True(t, strings.HasSuffix(err.Error(), "no such file or directory"))
	} else {
		assert.Fail(t, "should fail")
	}
	<-done
}

func (suite *ClientSuite) TestPostMultipartFormFileMimeType() {
	var actual []*partData
	var lock sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		mt, params, _ := mime.ParseMediaType(ct)
		w.Write([]byte(mt))
		mr := multipart.NewReader(r.Body, params["boundary"])
		lock.Lock()
		defer lock.Unlock()
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				return
			}
			actual = append(actual, newParDataFromPart(p))
		}
	})
	done := startServer(handler)

	f, _ := os.Open("testdata/cookies.txt")
	data, _ := ioutil.ReadAll(f)
	config.forms.Set(`name=@testdata/cookies.txt`)
	expected := []*partData{
		newPartData("name", "cookies.txt", "text/plain; charset=utf-8", string(data)),
	}
	config.method = http.MethodPost
	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	lock.Lock()
	defer lock.Unlock()
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		assert.Equal(t, "multipart/form-data", b.String())
		assert.Equal(t, expected, actual)
	}
	<-done
}

func (suite *ClientSuite) TestBenchmarkOK() {
	config.bmEnabled = true
	config.bmDuration = 100 * time.Millisecond
	config.bmConn = 4
	config.bmReqPerConn = 2
	count := int32(0)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Write([]byte("hello world"))
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		output := b.String()
		assert.True(t, strings.Contains(output, fmt.Sprintf("%d requests in ", count)),
			fmt.Sprintf("mismatch %d", count))
		assert.False(t, strings.Contains(output, "Errors:"))
		// print the output for debug purpose
		fmt.Println(output)
	}
	<-done
}

func (suite *ClientSuite) TestBenchmarkErr() {
	config.address = addrNotListened
	config.connectTimeout = 10 * time.Millisecond
	config.bmEnabled = true
	config.bmDuration = 50 * time.Millisecond
	config.bmConn = 4
	config.bmReqPerConn = 2

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		output := b.String()
		assert.True(t, strings.Contains(output, "Errors:"))
		fmt.Println(output)
	}
}

func (suite *ClientSuite) TestBenchmarkBadStatusCode() {
	config.bmEnabled = true
	config.bmDuration = 100 * time.Millisecond
	config.bmConn = 2
	config.bmReqPerConn = 1
	count := int32(0)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(404)
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		output := b.String()
		assert.True(t, strings.Contains(output, "requests in "))
		assert.False(t, strings.Contains(output, "Errors:"))
		assert.True(t, strings.Contains(output, fmt.Sprintf("Non-2xx or 3xx responses: %d", count)),
			fmt.Sprintf("mismatch %d", count))
		fmt.Println(output)
	}
	<-done
}

func (suite *ClientSuite) TestBenchmarkCancelled() {
	if *builtWithRace {
		// this is a known race, see the comment in benchmark.go
		fmt.Fprintln(os.Stderr, "Skip benchmark cancelled test when built with race detector")
		return
	}

	config.bmEnabled = true
	config.bmDuration = 100 * time.Second
	config.bmConn = 1
	config.bmReqPerConn = 2
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	done := startServer(handler)

	t := suite.T()
	b := &bytes.Buffer{}
	go func() {
		time.Sleep(100 * time.Millisecond)
		p, _ := os.FindProcess(syscall.Getpid())
		p.Signal(os.Interrupt)
	}()
	start := time.Now()
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Fail(t, err.Error())
	} else {
		output := b.String()
		assert.False(t, strings.Contains(output, "Errors:"))
		assert.False(t, time.Now().Sub(start).Seconds() > 10)
		fmt.Println(output)
	}
	<-done
}
