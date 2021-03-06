package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"io"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/url"

	"github.com/lucas-clemente/quic-go/h2quic"
)

var (
	currSNI = ""
)

func storeClientSNI(chi *tls.ClientHelloInfo) (*tls.Config, error) {
	// quic-go doesn't expose the underlying Conn
	currSNI = chi.ServerName
	return nil, nil
}

func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template,
		&template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key)},
	)
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{tlsCert},
		GetConfigForClient: storeClientSNI,
	}
}

var (
	tlsCfg = generateTLSConfig()
)

func startServer(addr string, handler http.Handler) {
	netAddr, err := url.Parse(addr)
	if err != nil {
		panic(err)
	}

	server := &h2quic.Server{
		Server: &http.Server{
			Addr:    netAddr.Host,
			Handler: handler,
		},
	}
	server.TLSConfig = tlsCfg
	err = server.Serve(nil)
	if err != nil {
		panic(err)
	}
}

func mustWrite(w io.Writer, p []byte) {
	_, err := w.Write(p)
	if err != nil {
		panic(err.Error())
	}
}

func mustWriteHeader(w io.Writer, h http.Header) {
	err := h.Write(w)
	if err != nil {
		panic(err.Error())
	}
}

func main() {
	port := ""
	flag.StringVar(&port, "l", "4443", "the port listened by echo back server")
	flag.Parse()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mustWrite(w, []byte("SNI: "+currSNI+"\r\n"))
		mustWrite(w, []byte(r.Method+" "+r.RequestURI+" "+r.Proto+"\r\n"))
		mustWrite(w, []byte("Host: "+r.Host+"\r\n"))
		mustWriteHeader(w, r.Header)
		mustWrite(w, []byte("\r\n"))
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			mustWrite(w, []byte(err.Error()))
			return
		}
		mustWrite(w, body)
		mustWriteHeader(w, r.Trailer)
	})
	startServer("https://127.0.0.1:"+port, handler)
}
