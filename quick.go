package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lucas-clemente/quic-go/h2quic"
)

var (
	insecure bool
	address  string
	sni      string
)

func fatal(format string, a ...interface{}) {
	fmt.Printf(format+"\n", a...)
	os.Exit(1)
}

func init() {
	flag.BoolVar(&insecure, "k", false, "Allow connections to SSL sites without certs")
	flag.StringVar(&sni, "sni", "", "Specify the SNI instead of using the host")
}

func checkArgs() error {
	flag.Parse()

	if flag.NArg() < 1 {
		return fmt.Errorf("no URL specified")
	}

	rawURL := flag.Arg(0)
	ok := strings.Contains(rawURL, "://")
	if !ok {
		// url.Parse doesn't accept a relative url without scheme, so we have
		// to do it ourselves. Note that we don't relative url without host.
		rawURL = "https://" + rawURL
	}

	uri, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if uri.Scheme == "" {
		uri.Scheme = "https"
	}
	if uri.Host == "" || uri.Scheme != "https" {
		return fmt.Errorf("URL invalid")
	}

	if sni == "" {
		sni = uri.Host
	}

	if strings.IndexByte(sni, ':') != -1 {
		hostname, _, err := net.SplitHostPort(sni)
		if err != nil {
			return err
		}

		sni = hostname
	}

	if uri.Port() == "" {
		if uri.Scheme == "https" {
			uri.Host += ":443"
		} else {
			return fmt.Errorf("port required in the URL")
		}
	}

	address = uri.String()

	return nil
}

const (
	defaultConnectTimeout = 1000 * time.Millisecond
)

func main() {
	err := checkArgs()
	if err != nil {
		fatal("failed to check args: %s", err.Error())
	}

	tlsConf := &tls.Config{
		InsecureSkipVerify: insecure,
		ServerName:         sni,
	}

	roundTripper := &h2quic.RoundTripper{
		TLSClientConfig: tlsConf,
	}
	defer roundTripper.Close()

	hclient := &http.Client{
		Transport: roundTripper,
	}

	resp, err := hclient.Get(address)
	if err != nil {
		fatal("failed to GET %s: %s", address, err.Error())
	}

	out := os.Stdout
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		fatal("failed to copy the output from %s to stdout: %s", address, err.Error())
	}
}
