package main

import (
	"errors"
	"io"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
)

type proxyHandler struct {
	ssh *sshTransport
}

func NewProxyHandler(ssh *sshTransport) *proxyHandler {
	return &proxyHandler{ssh: ssh}
}

func (ph *proxyHandler) ServeHTTP(rw http.ResponseWriter, origReq *http.Request) {
	proxyReq := NewProxyRequest(rw, origReq, ph.ssh.Transport)
	proxyReq.Handle()
}

type proxyRequest struct {
	rw               http.ResponseWriter
	origReq          *http.Request
	transport        http.RoundTripper
	requestedURL     string
	upstreamClient   *http.Client
	upstreamResponse *http.Response
	upstreamRequest  *http.Request
}

func NewProxyRequest(rw http.ResponseWriter, origReq *http.Request, transport http.RoundTripper) *proxyRequest {
	return &proxyRequest{
		rw:        rw,
		origReq:   origReq,
		transport: transport,
	}
}

func (pr *proxyRequest) Handle() {
	log.WithFields(log.Fields{
		"method": pr.origReq.Method,
		"url":    pr.origReq.URL,
		"proto":  pr.origReq.Proto}).Info("handling request")

	err := pr.validate()
	if err != nil {
		return
	}
	err = pr.buildRequest()
	if err != nil {
		return
	}
	err = pr.sendRequest()
	if err != nil {
		return
	}
	err = pr.forwardResponse()
	if err != nil {
		return
	}
}

func (pr *proxyRequest) validate() error {
	pr.requestedURL = pr.origReq.URL.String()
	if pr.origReq.Proto == "HTTP/1.0" && !strings.HasPrefix(pr.requestedURL, "http://") {
		pr.requestedURL = "http://" + pr.origReq.URL.String()
	}
	if !pr.origReq.URL.IsAbs() {
		log.WithFields(log.Fields{"url": pr.requestedURL}).Warn("rejecting non-proxy request")
		pr.rw.WriteHeader(http.StatusBadRequest)
		pr.rw.Write([]byte("Got non-proxy request.\n"))
		return errors.New("non-proxy request")
	}
	return nil
}

func (pr *proxyRequest) buildRequest() error {
	req, err := http.NewRequest(pr.origReq.Method, pr.requestedURL, nil)
	pr.upstreamRequest = req
	if err != nil {
		pr.rw.WriteHeader(http.StatusInternalServerError)
		log.Error("failed to create upstream request")
		return errors.New("request creation failure")
	}
	for k, vv := range pr.origReq.Header {
		if strings.HasPrefix(k, "Proxy-") {
			continue
		}
		if k == "Connection" {
			continue
		}
		for _, v := range vv {
			log.WithFields(log.Fields{"header": k, "value": v}).Debug("copying request header")
			pr.upstreamRequest.Header.Add(k, v)
		}
	}
	pr.upstreamRequest.Body = pr.origReq.Body
	pr.upstreamClient = &http.Client{
		Transport: pr.transport,
	}
	return nil
}

func (pr *proxyRequest) sendRequest() error {
	log.Debug("beginning http request")
	upstreamResponse, err := pr.upstreamClient.Do(pr.upstreamRequest)
	log.Debug("finished http request")
	if err != nil {
		pr.rw.WriteHeader(http.StatusBadGateway)
		log.WithFields(log.Fields{"err": err}).Error("upstream request failed")
		return errors.New("upstream request failed")
	}
	pr.upstreamResponse = upstreamResponse
	return nil
}

func (pr *proxyRequest) forwardResponse() error {
	respHeader := pr.rw.Header()
	for k, vv := range pr.upstreamResponse.Header {
		if k == "Content-Length" {
			continue
		}
		for _, v := range vv {
			log.WithFields(log.Fields{"header": k, "value": v}).Debug("copying response header")
			respHeader.Add(k, v)
		}
	}
	pr.rw.WriteHeader(pr.upstreamResponse.StatusCode)
	log.WithFields(log.Fields{"len": pr.upstreamResponse.ContentLength}).Debug("copying response body")
	_, err := io.Copy(pr.rw, pr.upstreamResponse.Body)
	if err != nil {
		log.WithFields(log.Fields{"err": err}).Error("failed to forward response body")
		return errors.New("failed to forward response body")
	}
	return nil
}
