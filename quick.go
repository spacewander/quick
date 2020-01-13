package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"

	quic "github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/h2quic"
)

const (
	version = "0.3.2"

	defaultMethod      = http.MethodGet
	defaultContentType = "application/json"
	formURLEncoded     = "application/x-www-form-urlencoded"
)

type headersValue struct {
	hdr http.Header
}

func (hv *headersValue) String() string {
	var b bytes.Buffer
	_ = hv.hdr.Write(&b)
	return string(bytes.TrimSuffix(b.Bytes(), crlf))
}

func (hv *headersValue) Set(value string) error {
	value = strings.TrimSpace(value)
	if colon := strings.IndexByte(value, ':'); colon != -1 && 0 < colon &&
		colon < len(value)-1 {
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
	outFilename     string

	insecure bool
	sni      string

	noRedirect bool

	connectTimeout time.Duration
	idleTimeout    time.Duration
	maxTime        time.Duration

	customHeaders headersValue
	revolver      resolveValue

	// originHost stores the normalized version of host passed in the uri argument
	originHost string
	address    string

	userAgent string
	method    string

	data        dataValue
	forms       formValue
	contentType string

	cookie     string
	loadCookie string
	dumpCookie string

	bmDuration   time.Duration
	bmConn       int
	bmReqPerConn int
	bmEnabled    bool
}

func newQuickConfig() *quickConfig {
	cfg := &quickConfig{
		// a timeout of zero means no timeout
		maxTime:        0,
		connectTimeout: 1000 * time.Millisecond,

		userAgent:     "quick/" + version,
		customHeaders: headersValue{hdr: http.Header{}},

		contentType: defaultContentType,
	}
	return cfg
}

var (
	config = newQuickConfig()

	crlf = []byte{'\r', '\n'}

	showVersion = false
)

func init() {
	flag.BoolVar(&config.headersIncluded, "i", config.headersIncluded,
		"Include response headers in the output")
	flag.BoolVar(&config.headersOnly, "I", config.headersOnly,
		"Show response headers only")
	flag.StringVar(&config.outFilename, "o", config.outFilename,
		"Write the response body to this file")
	flag.BoolVar(&config.insecure, "k", config.insecure,
		`Don't verify the certificates when connect to the server.
This is the default in benchmark mode.`)

	flag.BoolVar(&config.noRedirect, "no-redirect", config.noRedirect,
		"Don't follow redirect. This is the default in benchmark mode.")

	timeFmt := ", in the format like 1.5s"
	flag.DurationVar(&config.connectTimeout, "connect-timeout",
		config.connectTimeout,
		"Maximum time for the connect operation"+timeFmt)
	flag.DurationVar(&config.idleTimeout, "idle-timeout", config.idleTimeout,
		`Close connection if handshake successfully and no incoming network
activity in this duration. A reasonable duration will be chosed if not specified
or is set to zero.`)
	flag.DurationVar(&config.maxTime, "max-time", config.maxTime,
		"Maximum time for the whole operation"+timeFmt)

	flag.StringVar(&config.sni, "sni", config.sni,
		"Specify the SNI instead of using the host")
	flag.StringVar(&config.userAgent, "user-agent", config.userAgent,
		"Specify the User-Agent to use")
	flag.Var(&config.customHeaders, "H", "Pass custom header(s) to server")
	flag.Var(&config.revolver, "resolve",
		`Provide a custom address for a specific host and port pair in host:port:address
format. The address part can contain a new port to use. If the specific URL
doesn't contain a port, the port of the pair is 443`)
	flag.StringVar(&config.method, "X", config.method, "Specify request method")
	flag.Var(&config.data, "d", `Specify HTTP request body data.
If the request method is not specified, POST will be used.
If the Content-Type is not specified via -H, we will try to guess the Content-Type if there is
only one file to submit, otherwise `+config.contentType+" will be used.\n"+
		`Features like '@file' annotation and multiple body concatenation are supported.
Read the docs of curl to dive into the details.`)
	flag.Var(&config.forms, "F", `Send multipart/form-data request.
If the request method is not specified, POST will be used.
If the Content-Type is not specified via -H, multipart/form-data will be used.
Features like '@file' annotation, 'type='/'filename=' keywords are supported.
If 'type=' not given, we guess the form's Content-Type according to the
'filename=' keyword or the filename of the submitted file.
Features like 'headers=' keyword are not supported yet.
Read the docs of curl to dive into the details.
`)

	flag.StringVar(&config.cookie, "cookie", config.cookie,
		`Attach cookies to the request. The cookies should be in
'name=value; name=value...' format`)
	flag.StringVar(&config.loadCookie, "load-cookie", config.loadCookie,
		`Load cookies from the given file. The file should be in a format
described in http://www.cookiecentral.com/faq/#3.5`)
	flag.StringVar(&config.dumpCookie, "dump-cookie", config.dumpCookie,
		"Write cookies to the given file after operation")

	flag.DurationVar(&config.bmDuration, "bm-duration", config.bmDuration,
		"Duration of the benchmark")
	flag.IntVar(&config.bmConn, "bm-conn", config.bmConn,
		"Number of the connections in the benchmark")
	flag.IntVar(&config.bmReqPerConn, "bm-req-per-conn", config.bmReqPerConn,
		"Number of the requests to keep in a connection")

	flag.BoolVar(&showVersion, "version", false, "Show version and exit")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s [OPTIONS] URL
OPTIONS:
`, os.Args[0])
		flag.CommandLine.PrintDefaults()
	}

}

// for developer
var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

func checkArgs() error {
	flag.Parse()

	if showVersion {
		fmt.Println(version)
		versions := make([]string, len(SupportedVersions))
		for i := range versions {
			versions[i] = SupportedVersions[i].String()
		}
		fmt.Printf("Supported QUIC versions: %s\n", strings.Join(versions, ", "))
		os.Exit(0)
	}

	if flag.NArg() < 1 {
		return errors.New("no URL specified")
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
		return errors.New("URL invalid")
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

	uri.Host = resolveAddr(uri.Host, config)

	config.address = uri.String()

	maxTime := config.maxTime
	connectTimeout := config.connectTimeout
	idleTimeout := config.idleTimeout
	if maxTime < 0 {
		return fmt.Errorf(
			"invalid argument: -max-time should not be negative, got %v",
			maxTime)
	}

	if maxTime != 0 {
		if maxTime < connectTimeout {
			config.connectTimeout = maxTime
		}
		if maxTime < idleTimeout {
			config.idleTimeout = maxTime
		}
	}

	if connectTimeout <= 0 {
		return fmt.Errorf(
			"invalid argument: -connect-timeout should be positive, got %v",
			connectTimeout)
	}

	if idleTimeout < 0 {
		return fmt.Errorf(
			"invalid argument: -idle-timeout should not be negative, got %v",
			idleTimeout)
	}

	if config.data.Provided() && config.forms.Provided() {
		return errors.New("invalid argument: -d can't be used with -F")
	}

	if config.method == "" {
		if config.data.Provided() || config.forms.Provided() {
			config.method = http.MethodPost
		} else if config.headersOnly {
			config.method = http.MethodHead
		} else {
			config.method = defaultMethod
		}
	} else {
		config.method = strings.ToUpper(config.method)
		switch config.method {
		case http.MethodGet, http.MethodHead, http.MethodDelete,
			http.MethodPost, http.MethodPatch, http.MethodPut:
		case http.MethodConnect, http.MethodOptions, http.MethodTrace:
			return fmt.Errorf("invalid argument: method %s is unsupported",
				config.method)
		default:
			return fmt.Errorf("invalid argument: unknown method %s", config.method)
		}
	}

	if config.cookie != "" && config.loadCookie != "" {
		return errors.New("invalid argument: -cookie can't be used with -load-cookie")
	}

	ct := config.customHeaders.hdr.Get("Content-Type")
	if ct != "" {
		config.customHeaders.hdr.Del("Content-Type")
		config.contentType = ct
	}

	if config.bmConn > 0 && config.bmDuration > 0 && config.bmReqPerConn > 0 {
		config.bmEnabled = true
	}

	if config.bmEnabled {
		if config.dumpCookie != "" {
			return errors.New("unsupport option in benchmark mode")
		}
		if config.outFilename != "" || config.headersIncluded || config.headersOnly {
			return errors.New("output customization is not allowed in benchmark mode")
		}
		config.noRedirect = true
		config.insecure = true

		if config.maxTime == 0 {
			config.maxTime = config.bmDuration
		}
	}

	return nil
}

func dialWithTimeout(network, addr string, tlsCfg *tls.Config,
	cfg *quic.Config) (quic.Session, error) {

	ctx, cancel :=
		context.WithTimeout(context.Background(), config.connectTimeout)
	defer cancel()

	done := make(chan struct{})
	var sess quic.Session
	var err error
	go func() {
		sess, err = quic.DialAddrContext(ctx, addr, tlsCfg, cfg)
		close(done)
	}()

	select {
	case <-done:
		return sess, err
	case <-ctx.Done():
		return nil, errors.New("connect timeout")
	}
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

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

func fatal(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
	os.Exit(1)
}

func warn(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "Warning: "+format+"\n", a...)
}

func mustWrite(out io.Writer, p []byte) {
	_, err := out.Write(p)
	if err != nil {
		fatal(err.Error())
	}
}

func mustWriteString(out io.Writer, s string) {
	_, err := io.WriteString(out, s)
	if err != nil {
		fatal(err.Error())
	}
}

func createCookieManager() (CookieManager, error) {
	cm, err := newCookieManager()
	if err != nil {
		return nil, err
	}

	if config.cookie != "" {
		err = cm.LoadCookiesForURL(config.address, config.cookie)
	} else if config.loadCookie != "" {
		err = cm.Load(config.loadCookie)
	}

	return cm, err
}

func createClient(cm CookieManager) (*http.Client, error) {
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

	hclient := &http.Client{
		Jar:       cm.Jar(),
		Transport: roundTripper,
	}

	if config.noRedirect {
		hclient.CheckRedirect = noRedirect
	} else {
		hclient.CheckRedirect = redirectResolved
	}

	return hclient, nil
}

func destroyClient(hclient *http.Client) {
	roundTripper := hclient.Transport.(*h2quic.RoundTripper)
	roundTripper.Close()
}

func createReq(oldReq *http.Request) (*http.Request, context.CancelFunc, error) {
	var err error
	var body io.ReadCloser
	if config.data.Provided() || config.forms.Provided() {
		var ct string
		// need to create separate body reader for each request
		if config.data.Provided() {
			body, ct, err = config.data.Open(config.contentType)
		} else {
			body, ct, err = config.forms.Open()
		}
		if err != nil {
			return nil, nil, err
		}
		config.contentType = ct
	}

	var req *http.Request
	if oldReq == nil || body != nil {
		req, err = http.NewRequest(config.method, config.address, body)
		if err != nil {
			return nil, nil, err
		}

		req.Header.Set("User-Agent", config.userAgent)
		req.Header.Set("Content-Type", config.contentType)
		// the config.address may be changed via -resolve option, we need to
		// use the origin Host instead
		req.Header.Set("Host", config.originHost)
		for k, v := range config.customHeaders.hdr {
			req.Header[k] = v
		}
		if host := req.Header.Get("Host"); host != "" {
			req.Host = host
		}
	} else {
		req = oldReq
	}

	var cancel context.CancelFunc
	if config.maxTime > 0 {
		var ctx context.Context
		ctx, cancel = context.WithTimeout(context.Background(), config.maxTime)
		req = req.WithContext(ctx)
	}

	return req, cancel, nil
}

func readResp(req *http.Request, resp *http.Response, out io.Writer) error {
	headersIncluded := config.headersIncluded
	headersOnly := config.headersOnly
	if headersIncluded || headersOnly {
		// curl's -i/-I also shows response line, let's follow it
		mustWriteString(out, resp.Proto+" "+resp.Status)
		mustWrite(out, crlf)

		headers := make([]string, len(resp.Header))
		i := 0
		for k := range resp.Header {
			headers[i] = k
			i++
		}
		// make the output reproducible
		sort.Strings(headers)
		for _, k := range headers {
			v := resp.Header[k]
			for _, subv := range v {
				mustWriteString(out, k+": "+subv)
				mustWrite(out, crlf)
			}
		}
	}

	if headersOnly {
		return nil
	}

	if headersIncluded {
		mustWrite(out, crlf)
	}

	outFilename := config.outFilename
	if outFilename != "" {
		f, err := openFileToWrite(outFilename)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	if config.maxTime > 0 {
		resp.Body = &cancellableBody{
			rc:  resp.Body,
			ctx: req.Context(),
		}
	}

	defer resp.Body.Close()
	_, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy the output from %s: %s",
			config.address, err.Error())
	}

	return nil
}

func runInNormalMode(cm CookieManager, out io.Writer) error {
	hclient, err := createClient(cm)
	if err != nil {
		return err
	}
	defer destroyClient(hclient)

	req, cancel, err := createReq(nil)
	if err != nil {
		return err
	}
	if cancel != nil {
		defer cancel()
	}

	resp, err := hclient.Do(req)
	if err != nil {
		return err
	}

	if config.dumpCookie != "" {
		err = cm.Dump(config.dumpCookie)
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to dump cookie: "+err.Error())
		}
	}

	return readResp(req, resp, out)
}

func runInBenchmarkMode(cm CookieManager, out io.Writer) error {
	fmt.Fprintf(out,
		"Running %v test @ %s\n  %d connections and %d requests per connection\n",
		config.bmDuration,
		config.address,
		config.bmConn,
		config.bmReqPerConn,
	)

	conns := make([]*http.Client, config.bmConn)
	for i := 0; i < config.bmConn; i++ {
		hclient, err := createClient(cm)
		if err != nil {
			return err
		}
		defer destroyClient(hclient)
		conns[i] = hclient
	}

	stats := make([]*bmStat, config.bmConn)
	cancelled := make(chan struct{})
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		close(cancelled)
	}()

	var wg sync.WaitGroup
	wg.Add(config.bmConn)
	now := time.Now()
	for i := 0; i < config.bmConn; i++ {
		go runReqsInParallel(conns[i], &stats[i], &wg, cancelled)
	}
	wg.Wait()

	used := time.Since(now)
	printStats(used, stats, out)
	return nil
}

func run(out io.Writer) error {
	cm, err := createCookieManager()
	if err != nil {
		return err
	}

	if config.bmEnabled {
		return runInBenchmarkMode(cm, out)
	}
	return runInNormalMode(cm, out)
}

func main() {
	err := checkArgs()
	if err != nil {
		fatal(err.Error())
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fatal(err.Error())
		}
		err = pprof.StartCPUProfile(f)
		if err != nil {
			fatal(err.Error())
		}
		defer pprof.StopCPUProfile()
	}

	err = run(os.Stdout)
	if err != nil {
		fatal(err.Error())
	}
}
