package main

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

type dataValue struct {
	srcs []string
}

func (dv *dataValue) String() string {
	return strings.Join(dv.srcs, " ")
}

func (dv *dataValue) Set(value string) error {
	if value == "" {
		return fmt.Errorf("empty data not allowed")
	}
	if value[0] == '@' && len(value) == 1 {
		return fmt.Errorf("empty file name not allowed")
	}
	dv.srcs = append(dv.srcs, value)
	return nil
}

func (dv *dataValue) Provided() bool {
	return len(dv.srcs) > 0
}

func (dv *dataValue) Open(contentType string) (io.ReadCloser, string, error) {
	var readers []io.Reader
	if contentType == formURLEncoded {
		readers = make([]io.Reader, 2*len(dv.srcs)-1)
	} else {
		readers = make([]io.Reader, len(dv.srcs))
	}
	j := 0
	extType := ""
	for i, src := range dv.srcs {
		if i > 0 && contentType == formURLEncoded {
			// for this type, we need to use '&' to concat multiple inputs
			readers[j] = strings.NewReader("&")
			j++
		}
		if src[0] == '@' {
			var err error
			fn := src[1:]
			readers[j], err = os.Open(fn)
			if err != nil {
				for i = 0; i < j; i++ {
					if rc, ok := readers[i].(io.ReadCloser); ok {
						rc.Close()
					}
				}
				return nil, "", err
			}

			if j == 0 {
				ext := filepath.Ext(fn)
				extType = mime.TypeByExtension(ext)
			}
		} else {
			readers[j] = strings.NewReader(src)
		}
		j++
	}

	var ds dataSource
	if len(readers) == 1 {
		ds = dataSource{
			readers[0],
			readers,
		}
		if extType != "" {
			contentType = extType
		}
	} else {
		ds = dataSource{
			io.MultiReader(readers...),
			readers,
		}
	}

	return ds, contentType, nil
}

type dataSource struct {
	io.Reader
	readers []io.Reader
}

func (ds dataSource) Close() error {
	for _, r := range ds.readers {
		if rc, ok := r.(io.ReadCloser); ok {
			rc.Close()
			// ignore error since we are going to exit this process
		}
	}
	return nil
}
