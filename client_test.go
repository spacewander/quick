package main

// this file contains tests which run through between client and server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-clemente/quic-go/h2quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

const (
	addrNotListened = "https://127.0.0.1:11111"
	addrListened    = "https://127.0.0.1:28443"
)

type ClientSuite struct {
	suite.Suite
}

func (suite *ClientSuite) SetupTest() {
	config.address = addrListened
	config.insecure = true
}

func TestClientSuite(t *testing.T) {
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

var (
	tlsCfg = generateTLSConfig()
)

func startServer(handler http.Handler) chan struct{} {
	done := make(chan struct{})
	go func() {
		netAddr, err := url.Parse(addrListened)
		if err != nil {
			panic(err)
		}

		server := &h2quic.Server{
			Server: &http.Server{
				Addr:    netAddr.Host,
				Handler: handler,
			},
		}
		server.TLSConfig = tlsCfg

		go func() {
			server.Serve(nil)
		}()
		<-done
		err = server.Close()
		if err != nil {
			panic(err)
		}
		close(done)
	}()

	// ensure server is started
	time.Sleep(50 * time.Millisecond)

	return done
}

func (suite *ClientSuite) TestMaxTime() {
	config.connectTimeout = 20 * time.Millisecond
	config.maxTime = 30 * time.Millisecond

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
	config.connectTimeout = 20 * time.Millisecond
	config.maxTime = 30 * time.Millisecond

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

func (suite *ClientSuite) TestReadResponseHeaderOnly() {
	data := "hello world"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("data", data)
		w.Write(bytes.Repeat([]byte("test"), 1000))
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
		assert.True(t, bytes.Contains(b.Bytes(), []byte("HTTP/2.0 200 OK\r\n")))
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Data: "+data+"\r\n")))
		assert.False(t, bytes.Contains(b.Bytes(), []byte("\r\n\r\n")))
		assert.False(t, bytes.Contains(b.Bytes(), []byte("test")))
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

	config.method = "HEAD"
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

func (suite *ClientSuite) TestReadResponseHeaderOnlyWithHEAD() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("method", r.Method)
		w.Write(bytes.Repeat([]byte("test"), 1000))
	})
	done := startServer(handler)

	config.headersOnly = true
	config.method = "HEAD"

	t := suite.T()
	b := &bytes.Buffer{}
	err := run(b)
	done <- struct{}{}
	if err != nil {
		assert.Nil(t, err, err.Error())
	} else {
		assert.True(t, bytes.Contains(b.Bytes(), []byte("HTTP/2.0 200 OK\r\n")))
		assert.True(t, bytes.Contains(b.Bytes(), []byte("Method: HEAD\r\n")))
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

	config.method = "DELETE"
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
	config.method = "POST"
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

	config.method = "POST"
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
	config.method = "GET"
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
		assert.Equal(t, "open /x/y/z: no such file or directory", err.Error())
	} else {
		assert.Fail(t, "should fail")
	}
	<-done
}
