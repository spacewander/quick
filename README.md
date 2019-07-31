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

This tool allows you to communicate HTTP over QUIC server in curl way.
For example:

```
quick -H "Content-Type: application/x-www-form-urlencoded" -d @data_file -X PUT \
    -k -i -o resp_body.txt 127.0.0.1:8443
```

Note that `-X PUT` is used here instead of the curl style `-XPUT` because the
way to handle command line arguments is different between Go and curl.

Run `quick -h` to find more options.

## Installation


1. Require Go 1.12
2. Enable Go modules: `export GO111MODULE=on`
3. Run `go get -v github.com/spacewander/quick`

## This tool is not the same as curl!

Although I try to mimic curl via providing a group of similar APIs, this tool
is not a drop-in replacement. For example, the default Content-Type of `-d data`
is `application/json` but not `application/x-www-form-urlencoded`.

If the behavior of this tool is annoying (not because of the difference from curl),
please open an issue and let's find a solution.

## Is there '-v' or '-vv' option?

You can use environment variable `QUIC_GO_LOG_LEVEL=info` or `QUIC_GO_LOG_LEVEL=debug` instead.
This feature is provided by quic-go itself.
