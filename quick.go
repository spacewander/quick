package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	quic "github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/h2quic"
)

const (
	defaultConnectTimeout = 1000 * time.Millisecond
	defaultMaxTime        = 0 // a timeout of zero means no timeout

	version = "0.1-dev"
)

type headersValue struct {
	hdr http.Header
}

func (hv *headersValue) String() string {
	if hv == nil {
		return ""
	}
	var b bytes.Buffer
	hv.hdr.Write(&b)
	return string(bytes.TrimSuffix(b.Bytes(), crlf))
}

func (hv *headersValue) Set(value string) error {
	value = strings.TrimSpace(value)
	if colon := strings.IndexByte(value, ':'); colon != -1 && 0 < colon && colon < len(value)-1 {
		// if the provided header contains invalid character like '_',
		// it will be passed without rejection because it can be accepted by
		// http.Header.Add.
		name := strings.TrimSpace(value[:colon])
		val := strings.TrimSpace(value[colon+1:])
		hv.hdr.Add(name, val)
		return nil
	}
	return fmt.Errorf("invalid header: [%s]", value)
}

var (
	headersOnly     bool
	headersIncluded bool

	insecure bool
	sni      string

	connectTimeout time.Duration
	idleTimeout    time.Duration
	maxTime        time.Duration

	customHeaders headersValue

	address string

	userAgent string

	crlf = []byte{'\r', '\n'}
)

func init() {
	timeFmt := ", in the format like 1.5s"
	flag.BoolVar(&headersIncluded, "i", false, "Include response headers in the output")
	flag.BoolVar(&headersOnly, "I", false, "Show response headers only")
	flag.BoolVar(&insecure, "k", false, "Allow connections to SSL sites without certs")
	flag.DurationVar(&connectTimeout, "connect-timeout", defaultConnectTimeout,
		"Maximum time for the connect operation"+timeFmt)
	flag.DurationVar(&idleTimeout, "idle-timeout", 0,
		"Close connection if handshake successfully and no incoming network activity in this duration.\n"+
			"A reasonable duration will be chosed if not specified.")
	flag.DurationVar(&maxTime, "max-time", defaultMaxTime,
		"Maximum time for the whole operation"+timeFmt)
	flag.StringVar(&sni, "sni", "", "Specify the SNI instead of using the host")
	flag.StringVar(&userAgent, "user-agent", "quick/"+version, "Specify the User-Agent to use")
	flag.Var(&customHeaders, "H", "Pass custom header(s) to server")
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

	if maxTime < 0 {
		return fmt.Errorf(
			"invalid argument: -max-time should not be negative, got %v", maxTime)
	}

	if maxTime != 0 && (maxTime < connectTimeout || maxTime < idleTimeout) {
		return fmt.Errorf(
			"invalid argument: -max-time should be larger than other timeouts")
	}

	if connectTimeout <= 0 {
		return fmt.Errorf(
			"invalid argument: -connect-timeout should be positive, got %v", connectTimeout)
	}

	return nil
}

func dialWithTimeout(network, addr string, tlsCfg *tls.Config,
	cfg *quic.Config) (sess quic.Session, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		sess, err = quic.DialAddrContext(ctx, addr, tlsCfg, cfg)
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return nil, fmt.Errorf("connect timeout")
	}

	return sess, err
}

type cancellableBody struct {
	rc  io.ReadCloser
	ctx context.Context
}

func (b *cancellableBody) Read(p []byte) (n int, err error) {
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	default:
	}
	return b.rc.Read(p)
}

func (b *cancellableBody) Close() error {
	err := b.rc.Close()
	return err
}
func run(out io.Writer) error {
	quicConf := &quic.Config{
		IdleTimeout: idleTimeout,
	}
	tlsConf := &tls.Config{
		InsecureSkipVerify: insecure,
		ServerName:         sni,
	}

	roundTripper := &h2quic.RoundTripper{
		QuicConfig:      quicConf,
		TLSClientConfig: tlsConf,
		Dial:            dialWithTimeout,
	}
	defer roundTripper.Close()

	hclient := &http.Client{
		Transport: roundTripper,
	}

	req, err := http.NewRequest("GET", address, nil)
	var ctx context.Context
	if maxTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), maxTime)
		defer cancel()
		req = req.WithContext(ctx)
	}

	req.Header.Set("User-Agent", userAgent)
	for k, v := range customHeaders.hdr {
		req.Header[k] = v
	}

	resp, err := hclient.Do(req)
	if err != nil {
		return err
	}

	if headersIncluded || headersOnly {
		// curl's -i/-I also shows response line, let's follow it
		io.WriteString(out, resp.Proto+" "+resp.Status)
		out.Write(crlf)

		headers := make([]string, len(resp.Header))
		i := 0
		for k := range resp.Header {
			headers[i] = k
			i++
		}
		// make the output reproducible
		sort.Sort(sort.StringSlice(headers))
		for _, k := range headers {
			v := resp.Header[k]
			io.WriteString(out, k+": "+strings.Join(v, ","))
			out.Write(crlf)
		}
	}

	if headersOnly {
		return nil
	}

	if headersIncluded {
		out.Write(crlf)
	}

	if maxTime > 0 {
		resp.Body = &cancellableBody{
			rc:  resp.Body,
			ctx: ctx,
		}
	}

	defer resp.Body.Close()
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy the output from %s: %s", address, err.Error())
	}

	return nil
}

func fatal(format string, a ...interface{}) {
	fmt.Printf(format+"\n", a...)
	os.Exit(1)
}

func main() {
	err := checkArgs()
	if err != nil {
		fatal(err.Error())
	}

	err = run(os.Stdout)
	if err != nil {
		fatal(err.Error())
	}
}
