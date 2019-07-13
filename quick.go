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
	version = "0.1-dev"
)

type headersValue struct {
	hdr http.Header
}

func (hv *headersValue) String() string {
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

type quickConfig struct {
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
	method    string
}

func newQuickConfig() *quickConfig {
	cfg := &quickConfig{
		// a timeout of zero means no timeout
		maxTime:        0,
		connectTimeout: 1000 * time.Millisecond,

		userAgent:     "quick/" + version,
		customHeaders: headersValue{hdr: http.Header{}},
		method:        "GET",
	}
	return cfg
}

var (
	config = newQuickConfig()

	crlf = []byte{'\r', '\n'}
)

func init() {
	flag.BoolVar(&config.headersIncluded, "i", config.headersIncluded, "Include response headers in the output")
	flag.BoolVar(&config.headersOnly, "I", config.headersOnly, "Show response headers only")
	flag.BoolVar(&config.insecure, "k", config.insecure, "Allow connections to SSL sites without certs")

	timeFmt := ", in the format like 1.5s"
	flag.DurationVar(&config.connectTimeout, "connect-timeout", config.connectTimeout,
		"Maximum time for the connect operation"+timeFmt)
	flag.DurationVar(&config.idleTimeout, "idle-timeout", config.idleTimeout,
		"Close connection if handshake successfully and no incoming network activity in this duration.\n"+
			"A reasonable duration will be chosed if not specified or is set to zero.")
	flag.DurationVar(&config.maxTime, "max-time", config.maxTime,
		"Maximum time for the whole operation"+timeFmt)

	flag.StringVar(&config.sni, "sni", config.sni, "Specify the SNI instead of using the host")
	flag.StringVar(&config.userAgent, "user-agent", config.userAgent, "Specify the User-Agent to use")
	flag.Var(&config.customHeaders, "H", "Pass custom header(s) to server")
	flag.StringVar(&config.method, "X", config.method, "Specify request method")
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
	if uri.Host == "" || uri.Scheme != "https" {
		return fmt.Errorf("URL invalid")
	}

	if config.sni == "" {
		config.sni = uri.Host
	}

	if strings.IndexByte(config.sni, ':') != -1 {
		hostname, _, err := net.SplitHostPort(config.sni)
		if err != nil {
			return err
		}

		config.sni = hostname
	}

	if uri.Port() == "" {
		uri.Host += ":443"
	}

	config.address = uri.String()

	maxTime := config.maxTime
	connectTimeout := config.connectTimeout
	idleTimeout := config.idleTimeout
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

	if idleTimeout < 0 {
		return fmt.Errorf(
			"invalid argument: -idle-timeout should not be negative, got %v", idleTimeout)
	}

	config.method = strings.ToUpper(config.method)
	switch config.method {
	case "GET", "HEAD", "DELETE":
	case "CONNECT", "OPTIONS", "POST", "PUT", "PATCH", "TRACE":
		return fmt.Errorf("invalid argument: method %s is unsupported",
			config.method)
	default:
		return fmt.Errorf("invalid argument: unknown method %s", config.method)
	}

	return nil
}

func dialWithTimeout(network, addr string, tlsCfg *tls.Config,
	cfg *quic.Config) (sess quic.Session, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), config.connectTimeout)
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
		IdleTimeout: config.idleTimeout,
	}
	tlsConf := &tls.Config{
		InsecureSkipVerify: config.insecure,
		ServerName:         config.sni,
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

	req, err := http.NewRequest(config.method, config.address, nil)
	var ctx context.Context
	if config.maxTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), config.maxTime)
		defer cancel()
		req = req.WithContext(ctx)
	}

	req.Header.Set("User-Agent", config.userAgent)
	for k, v := range config.customHeaders.hdr {
		req.Header[k] = v
	}

	resp, err := hclient.Do(req)
	if err != nil {
		return err
	}

	headersIncluded := config.headersIncluded
	headersOnly := config.headersOnly
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

	if config.maxTime > 0 {
		resp.Body = &cancellableBody{
			rc:  resp.Body,
			ctx: ctx,
		}
	}

	defer resp.Body.Close()
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy the output from %s: %s",
			config.address, err.Error())
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
