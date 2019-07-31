package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spacewander/quick/cookiejar"
	"golang.org/x/net/publicsuffix"
)

// CookieManager wraps a cookie jar and provides API to persist cookie
type CookieManager interface {
	// Dump dumps all cookies to file
	Dump(fn string) error
	// Load loads all cookies from file
	Load(fn string) error
	// LoadCookiesForURL parses a cookie string and attaches it to the given URL
	LoadCookiesForURL(url, cookie string) error
	// Jar returns the wrapped cookie jar
	Jar() *cookiejar.Jar
}

type cookieManager struct {
	jar    *cookiejar.Jar
	urls   map[string]*url.URL
	curURL *url.URL
}

func newCookieManager() (CookieManager, error) {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}

	return &cookieManager{
		jar:  jar,
		urls: map[string]*url.URL{},
	}, nil
}

func (cm cookieManager) Dump(fn string) error {
	f, err := openFileToWrite(fn)
	if err != nil {
		return err
	}
	defer f.Close()

	return cm.dump(f, true)
}

func (cm *cookieManager) Load(fn string) error {
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer f.Close()

	return cm.load(f)
}

func (cm *cookieManager) LoadCookiesForURL(url, cookies string) error {
	err := cm.addURL(url)
	if err != nil {
		return err
	}
	return cm.loads(cookies)
}

func (cm cookieManager) Jar() *cookiejar.Jar {
	return cm.jar
}

func (cm *cookieManager) addURL(rawURL string) error {
	var err error
	url, found := cm.urls[rawURL]
	if !found {
		url, err = url.Parse(rawURL)
		if err != nil {
			return err
		}
		cm.urls[rawURL] = url
	}
	cm.curURL = url
	return nil
}

/*
	curl cookie format, copied from http://www.cookiecentral.com/faq/#3.5

	An example cookies.txt file may have an entry that looks like this:

	.netscape.com     TRUE   /  FALSE  946684799   NETSCAPE_ID  100103

	Each line represents a single piece of stored information. A tab is inserted between each of the fields.

	From left-to-right, here is what each field represents:

	domain - The domain that created AND that can read the variable.
	flag - A TRUE/FALSE value indicating if all machines within a given domain can access the variable. This value is set automatically by the browser, depending on the value you set for domain.
	path - The path within the domain that the variable is valid for.
	secure - A TRUE/FALSE value indicating if a secure connection with the domain is needed to access the variable.
	expiration - The UNIX time that the variable will expire on. UNIX time is defined as the number of seconds since Jan 1, 1970 00:00:00 GMT.
	name - The name of the variable.
	value - The value of the variable.
*/
func (cm cookieManager) dump(w io.Writer, trailingWS bool) error {
	cks := cm.jar.DumpCookies()
	lastOne := len(cks) - 1
	for i, ck := range cks {
		b := &bytes.Buffer{}
		b.WriteString(ck.Domain)
		b.WriteByte('\t')
		b.WriteString("TRUE")
		b.WriteByte('\t')
		b.WriteString(ck.Path)
		b.WriteByte('\t')
		if ck.Secure {
			b.WriteString("TRUE")
		} else {
			b.WriteString("FALSE")
		}
		b.WriteByte('\t')
		b.WriteString(strconv.FormatInt(ck.Expires.Unix(), 10))
		b.WriteByte('\t')
		b.WriteString(ck.Name)
		if ck.Value != "" || trailingWS {
			b.WriteByte('\t')
			b.WriteString(ck.Value)
		}
		if trailingWS || i < lastOne {
			b.WriteByte('\n')
		}
		_, err := w.Write(b.Bytes())
		if err != nil {
			return err
		}
	}
	return nil
}

func (cm *cookieManager) load(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	curDomain := ""
	curPath := ""
	curCookies := []*http.Cookie{}

	for scanner.Scan() {
		s := scanner.Text()
		if len(s) == 0 {
			continue
		}
		if s[0] == '#' {
			continue
		}

		// we don't require everyone to use tab correctly
		fields := strings.Fields(s)
		fieldLen := len(fields)
		if fieldLen < 6 {
			return fmt.Errorf("invalid cookie entry(%s): not enough fields", s)
		}
		domain := fields[0]
		u, err := url.Parse("https://" + domain)
		if err != nil || u.Hostname() != domain {
			return fmt.Errorf("invalid cookie entry(%s): invalid domain", s)
		}

		//skip flag := fields[1], we don't care about it
		path := fields[2]

		var secure bool
		if fields[3] == "TRUE" {
			secure = true
		} else if fields[3] == "FALSE" {
			secure = false
		} else {
			return fmt.Errorf("invalid cookie entry(%s): unrecognized secure", s)
		}

		expiration, err := strconv.Atoi(fields[4])
		if err != nil {
			return fmt.Errorf("invalid cookie entry(%s): %s",
				s, err.Error())
		}

		name := fields[5]

		var value string
		if fieldLen > 6 {
			value = fields[6]
		} else {
			value = ""
		}

		if domain != curDomain || path != curPath {
			if len(curCookies) > 0 {
				// not the first loop
				url := &url.URL{}

				if curDomain[0] == '.' {
					url.Host = curDomain[1:]
				} else {
					url.Host = curDomain
				}

				url.Path = curPath
				// only https scheme is supported
				url.Scheme = "https"

				// the URL is valid after we parsed the domain
				_ = cm.addURL(url.String())
				cm.jar.SetCookies(url, curCookies)
				// cookiejar.Jar won't own the cookies
				curCookies = curCookies[:0]
			}

			curDomain = domain
			curPath = path
		}

		ck := &http.Cookie{
			Name:     name,
			Value:    value,
			Path:     path,
			Domain:   domain,
			Expires:  time.Unix(int64(expiration), 0),
			Secure:   secure,
			HttpOnly: true,
		}
		curCookies = append(curCookies, ck)
	}

	if len(curCookies) > 0 {
		url := &url.URL{}

		if curDomain[0] == '.' {
			url.Host = curDomain[1:]
		} else {
			url.Host = curDomain
		}

		url.Path = curPath
		url.Scheme = "https"

		_ = cm.addURL(url.String())
		cm.jar.SetCookies(url, curCookies)
	}
	// we don't need to recover the cookie jar if the scan failed.
	return scanner.Err()
}

// loads "name=value; name=value" format string and associate parsed entries
// with the current URL
func (cm *cookieManager) loads(s string) error {
	if cm.curURL == nil {
		panic("should set the current URL")
	}

	cookies, ok := cookiejar.ReadCookies(s)
	if !ok {
		return fmt.Errorf("invalid cookies string")
	}
	cm.jar.SetCookies(cm.curURL, cookies)
	return nil
}

func (cm cookieManager) String() string {
	var b strings.Builder
	_ = cm.dump(&b, false)
	return b.String()
}
