package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type resolveValue struct {
	// the number of resolved addresses are so small that a slice is faster
	addrs [][]string
}

func (rv *resolveValue) String() string {
	pairs := make([]string, len(rv.addrs))
	for i, pair := range rv.addrs {
		pairs[i] = pair[0] + ":" + pair[1]
	}
	return strings.Join(pairs, " ")
}

const missingPort = "missing port in address"

func (rv *resolveValue) Set(value string) error {
	size := len(value)
	if size == 0 {
		return fmt.Errorf("invalid resolve: [%s]", value)
	}
	var res []string
	if value[0] == '[' {
		right := strings.IndexByte(value, ']')
		if right == -1 || right >= size-2 {
			return fmt.Errorf("invalid resolve: [%s]", value)
		}
		res = strings.SplitN(value[right+1:], ":", 3)
		res[0] = value[:right+1]
	} else {
		res = strings.SplitN(value, ":", 3)
	}
	if len(res) < 3 {
		return fmt.Errorf("invalid resolve: [%s]", value)
	}

	if i, err := strconv.Atoi(res[1]); err != nil || !(0 < i && i < 65536) {
		return fmt.Errorf("invalid resolve: [%s]", value)
	}
	src := res[0] + ":" + res[1]
	var dst string
	_, _, err := net.SplitHostPort(res[2])
	if err != nil {
		addrErr := err.(*net.AddrError)
		if addrErr.Err != missingPort {
			return fmt.Errorf("invalid resolve: [%s]", value)
		}
		dst = res[2] + ":" + res[1]
	} else {
		dst = res[2]
	}
	// prepend so the later one wins
	rv.addrs = append([][]string{[]string{src, dst}}, rv.addrs...)
	return nil
}

func resolveAddr(host string, config *quickConfig) string {
	if h, p, _ := net.SplitHostPort(host); p == "443" {
		config.originHost = h
	} else {
		config.originHost = host
	}
	for _, pair := range config.revolver.addrs {
		if pair[0] == host {
			return pair[1]
		}
	}

	return host
}

// copied from Go's source code
func refererForURL(lastReq, newReq *url.URL) string {
	if lastReq.Scheme == "https" && newReq.Scheme == "http" {
		return ""
	}
	referer := lastReq.String()
	if lastReq.User != nil {
		auth := lastReq.User.String() + "@"
		referer = strings.Replace(referer, auth, "", 1)
	}
	return referer
}

func redirectResolved(req *http.Request, via []*http.Request) error {
	// copy from client.go#defaultCheckRedirect
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}

	host := req.URL.Host
	if req.URL.Port() == "" {
		scheme := req.URL.Scheme
		if scheme != "" && scheme != "https" {
			return fmt.Errorf("unsupported scheme %s in redirect", scheme)
		}
		host += ":443"
	}
	originHost := config.originHost
	newHost := resolveAddr(host, config)
	if newHost != host {
		preReqURL := via[len(via)-1].URL
		preReqURL.Host = originHost
		if ref := refererForURL(preReqURL, req.URL); ref != "" {
			req.Header.Set("Referer", ref)
		}
		req.URL.Host = newHost
		req.Host = newHost
	}
	return nil
}
