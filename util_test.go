package main

import (
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/lucas-clemente/quic-go/h2quic"
)

const (
	addrListened = "https://127.0.0.1:28443"
)

func createTmpFile(content string) (f *os.File, fn string) {
	tmpfile, err := ioutil.TempFile("", "quick")
	if err != nil {
		panic(err)
	}

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		panic(err)
	}
	if err := tmpfile.Close(); err != nil {
		panic(err)
	}

	return tmpfile, tmpfile.Name()
}

func createTmpDir() (dir string) {
	dir, err := ioutil.TempDir("", "quick")
	if err != nil {
		panic(err)
	}

	return dir
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
