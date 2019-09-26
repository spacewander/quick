# Quick

Like curl but for HTTP over QUIC

[![Travis](https://travis-ci.org/spacewander/quick.svg?branch=master)](https://travis-ci.org/spacewander/quick)
[![codecov.io](https://codecov.io/github/spacewander/quick/coverage.svg?branch=master)](https://codecov.io/github/spacewander/quick?branch=master)
[![license](https://img.shields.io/badge/License-GPLv3-green.svg)](https://github.com/spacewander/quick/blob/master/LICENSE)

## Which version of QUIC is supported by this tool?

By using `quic-go v0.10.2`, this tool supports gQUIC 39/43/44, which are also
supported by latest Chrome and [Caddy](https://github.com/caddyserver/caddy)
when I start to write this documentation.

Why not support latest iQUIC? The QUIC protocol is changing rapidly (break changes
warning!). Maybe I will start to support iQUIC when the protocol is stable, or
maybe someone will send me a pull request.

Note that this tool doesn't support HTTP3. The HTTP over QUIC implemented by this
version of quic-go is different from HTTP3. The HTTP3 is still a draft, and I
will start to support it (via upgrading quic-go?) once the protocol is stable.

## Feature

### Normal mode

This tool allows you to communicate HTTP over QUIC server in curl way.
For example:

```
quick -H "Content-Type: application/x-www-form-urlencoded" -d @data_file -X PUT \
    -k -i -o resp_body.txt 127.0.0.1:8443
```

Note that `-X PUT` is used here instead of the curl style `-XPUT` because the
way to handle command line arguments is different between Go and curl.

Run `quick -h` to find more options.

### Benchmark mode

This tool allows you to do benchmark with a HTTP over QUIC server.
For instance:

```
$ quick -bm-duration 30s -bm-conn 2 -bm-req-per-conn 4 www.test.com:8443
Running 30s test @ https://www.test.com:8443
  2 connections and 4 requests per connection
  13872 requests in 30.070340458s
        Item      Avg      Stdev       Max   +/-Stdev
     Latency  17.31ms    41.11ms  574.53ms     98.83%
  Latency Distribution
    50.0%       12.57ms
    75.0%       15.38ms
    90.0%       21.54ms
    95.0%       26.71ms
    99.0%       66.15ms
    99.5%       506.51ms
    99.9%       538.54ms
  Non-2xx or 3xx responses: 13872
Requests/sec:    461.318355
```

To enable the benchmark mode, please specify `-bm-duration` and `-bm-conn` and
`-bm-reqs-per-conn`.

Most of the arguments in the normal mode can be used in the benchmark mode too.
(except `-o`, `-i`, `-I` and `-dump-cookie`)

Note that `quick` doesn't verify the targer server's cerificate and doesn't redirect
the request during the benchmark.

## Installation


1. Require Go 1.12
2. Enable Go modules: `export GO111MODULE=on`
3. Run `go get -v github.com/spacewander/quick`

## The HTTP3 support of curl is under development, why I need to use this tool?

While curl is trying to support HTTP3 (draft), this tool supports HTTP over QUIC.
The HTTP over QUIC(gQUIC actually) which is used by Chrome and Caddy can't
communicate with the HTTP3 (draft). So you might need this tool to send request
to some HTTP servers use QUIC(gQUIC actually).

Once the HTTP3 is no longer a draft, both curl and this tool will support HTTP3.
If you want to use the latest version of a command line HTTP3 client, this tool
is eaiser to install, though building curl from source is easy too.

By the way, this tool also allow you to do benchmark via the benchmark mode,
which is definitely not a feature provided by curl.

## This tool is not the same as curl!

Although I try to mimic curl via providing a group of similar APIs, this tool
is not a drop-in replacement. For example, the default Content-Type of `-d data`
is `application/json` but not `application/x-www-form-urlencoded`.

If the behavior of this tool is annoying (not because of the difference from curl),
please open an issue and let's find a solution.

## Is there '-v' or '-vv' option?

You can use environment variable `QUIC_GO_LOG_LEVEL=info` or `QUIC_GO_LOG_LEVEL=debug` instead.
This feature is provided by quic-go itself.
