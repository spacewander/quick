package main

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	octetStream = "application/octet-stream"
)

type form struct {
	name        string
	contentType string
	filename    string
	data        string
	fromFile    bool
}

func (f *form) String() string {
	s := "name=" + f.name
	if f.filename != "" {
		s += ";filename=" + f.filename
	}
	if f.contentType != "" {
		s += ";type=" + f.contentType
	}
	s += ";data=" + f.data
	return s
}

type formValue struct {
	forms []*form
}

func (fv *formValue) String() string {
	s := ""
	for i, f := range fv.forms {
		if i > 0 {
			s += "\n"
		}
		s += f.String()
	}
	return s
}

const (
	parsingKey = iota
	parsingVal
	parsingQuotedVal
	parsingSep
)

func (fv *formValue) Set(raw string) error {
	state := parsingKey
	start := 0
	needUnescape := false
	tokens := []string{}
	value := raw
	if value[len(value)-1] != ';' {
		value += ";"
	}

	for i, r := range value {
		if r == '=' {
			switch state {
			case parsingKey:
				tokens = append(tokens, value[start:i])
				start = i + 1
				state = parsingVal
			}
		} else if r == '"' {
			switch state {
			case parsingVal:
				if value[i-1] == '=' {
					start = i + 1
					state = parsingQuotedVal
				} else if value[i-1] == '"' {
					needUnescape = true
				}
			case parsingQuotedVal:
				if value[i-1] != '\\' {
					piece := unescapeIfNeeded(value[start:i], &needUnescape)
					tokens = append(tokens, piece)
					state = parsingSep
				} else {
					needUnescape = true
				}
			}
		} else if r == ';' {
			switch state {
			case parsingVal:
				piece := unescapeIfNeeded(value[start:i], &needUnescape)
				tokens = append(tokens, piece)
				state = parsingSep
			}
		} else if !unicode.IsSpace(r) {
			switch state {
			case parsingSep:
				start = i
				state = parsingKey
			}
		}
	}

	size := len(tokens)
	if size < 2 {
		return fmt.Errorf("invalid form: [%s]", raw)
	}

	f := &form{}
	for i := 0; i < size; i += 2 {
		key := tokens[i]
		value := tokens[i+1]
		switch key {
		case "filename":
			f.filename = value
		case "type":
			f.contentType = value
		default:
			if f.name == "" {
				f.name = key
				f.data = value
			} else {
				warn("skip unknown form field: %s=%s", key, value)
			}
		}
	}
	if f.name == "" {
		return fmt.Errorf("invalid form: [%s]", raw)
	}
	if f.data != "" && f.data[0] == '@' {
		if len(f.data) == 1 {
			return fmt.Errorf("invalid form: [%s]", raw)
		}
		f.fromFile = true
		f.data = f.data[1:]
		if f.filename == "" {
			f.filename = filepath.Base(f.data)
		}
	}

	fv.forms = append(fv.forms, f)
	return nil
}

// only for test, ignore len(fv.forms) == 0
func (fv *formValue) lastForm() *form {
	size := len(fv.forms)
	return fv.forms[size-1]
}

func (fv *formValue) Provided() bool {
	return len(fv.forms) > 0
}

var (
	quoteEscaper   *strings.Replacer
	quoteUnescaper *strings.Replacer
)

func escapeQuotes(s string) string {
	if quoteEscaper == nil {
		quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")
	}
	return quoteEscaper.Replace(s)
}

func unescapeQuotes(s string) string {
	if quoteUnescaper == nil {
		quoteUnescaper = strings.NewReplacer("\\\\", "\\", "\\\"", `"`)
	}
	return quoteUnescaper.Replace(s)
}

func unescapeIfNeeded(s string, need *bool) string {
	if *need {
		*need = false
		return unescapeQuotes(s)
	}
	return s
}

func (fv *formValue) Open() (io.ReadCloser, string, error) {
	pipeR, pipeW := io.Pipe()
	multipartW := multipart.NewWriter(pipeW)
	go func() {
		for _, form := range fv.forms {
			h := make(textproto.MIMEHeader)
			extType := ""
			if form.filename != "" {
				h.Set("Content-Disposition",
					fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
						escapeQuotes(form.name), escapeQuotes(form.filename)))
				ext := filepath.Ext(form.filename)
				extType = mime.TypeByExtension(ext)
			} else {
				h.Set("Content-Disposition",
					fmt.Sprintf(`form-data; name="%s"`, escapeQuotes(form.name)))
			}

			if form.contentType != "" {
				h.Set("Content-Type", form.contentType)
			} else if extType != "" {
				h.Set("Content-Type", extType)
			} else if form.fromFile {
				h.Set("Content-Type", octetStream)
			}

			partW, err := multipartW.CreatePart(h)
			if err != nil {
				_ = pipeW.CloseWithError(err)
				return
			}

			if !form.fromFile {
				_, err = partW.Write([]byte(form.data))
			} else {
				var fileR *os.File
				fileR, err = os.Open(form.data)
				if err != nil {
					_ = pipeW.CloseWithError(err)
					return
				}
				_, err = io.Copy(partW, fileR)
				fileR.Close()
			}

			if err != nil {
				_ = pipeW.CloseWithError(err)
				return
			}
		}
		// Avoid closing the pipe twice by calling the Close in every path.
		// Otherwise, the error will be overrided in Go <1.14
		pipeW.Close()
	}()

	fs := formSource{
		pipeR,
	}
	return fs, multipartW.FormDataContentType(), nil
}

type formSource struct {
	io.ReadCloser
}
