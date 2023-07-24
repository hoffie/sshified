package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
	err := proxyReq.Handle()
	if err != nil {
		log.WithFields(log.Fields{
			"method": origReq.Method,
			"url":    origReq.URL,
			"proto":  origReq.Proto,
			"err":    err,
		}).Debug("request failed")
	}
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

func (pr *proxyRequest) Handle() error {
	metricRequestsTotal.Inc()
	timer := prometheus.NewTimer(metricRequestDuration)
	defer timer.ObserveDuration()
	pr.buildURL()
	log.WithFields(log.Fields{
		"method": pr.origReq.Method,
		"url":    pr.requestedURL,
		"proto":  pr.origReq.Proto}).Trace("handling request")

	err := pr.buildRequest()
	if err != nil {
		metricRequestsFailedTotal.Inc()
		return err
	}
	err = pr.sendRequest()
	if err != nil {
		metricRequestsFailedTotal.Inc()
		return err
	}
	err = pr.forwardResponse()
	if err != nil {
		metricRequestsFailedTotal.Inc()
		return err
	}
	return nil
}

func (pr *proxyRequest) buildURL() {
	https := pr.origReq.URL.Query().Get("__sshified_use_insecure_https")
	if https == "" {
		pr.origReq.URL.Scheme = "http"
	} else {
		// TLSClientConfig is hardcoded and re-used in sshtransport.go
		pr.origReq.URL.Scheme = "https"
		values := pr.origReq.URL.Query()
		for k := range values {
			if strings.HasPrefix(k, "__sshified_") {
				values.Del(k)
			}
		}
		pr.origReq.URL.RawQuery = values.Encode()
	}
	pr.origReq.URL.Host = pr.origReq.Host
	pr.requestedURL = pr.origReq.URL.String()
}

func (pr *proxyRequest) buildRequest() error {
	log.WithFields(log.Fields{"method": pr.origReq.Method, "url": pr.requestedURL}).Trace("building upstream request")
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
			log.WithFields(log.Fields{"header": k, "value": v}).Trace("copying request header")
			pr.upstreamRequest.Header.Add(k, v)
		}
	}
	pr.upstreamRequest.Body = pr.origReq.Body
	pr.upstreamClient = &http.Client{
		Transport: pr.transport,
		Timeout:   time.Duration(*timeout) * time.Second,
	}
	return nil
}

func (pr *proxyRequest) sendRequest() error {
	log.Trace("beginning http request")
	upstreamResponse, err := pr.upstreamClient.Do(pr.upstreamRequest)
	log.Trace("finished http request")
	if err != nil {
		pr.rw.WriteHeader(http.StatusBadGateway)
		log.WithFields(log.Fields{"err": err}).Debug("upstream request failed")
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
			log.WithFields(log.Fields{"header": k, "value": v}).Trace("copying response header")
			respHeader.Add(k, v)
		}
	}
	pr.rw.WriteHeader(pr.upstreamResponse.StatusCode)
	log.Trace("copying response body")
	length, err := io.Copy(pr.rw, pr.upstreamResponse.Body)
	if err != nil {
		log.WithFields(log.Fields{"err": err}).Debug("failed to forward response body")
		return errors.New("failed to forward response body")
	}
	log.WithFields(log.Fields{"len": length}).Trace("done with copying response body")
	metricPayloadBytes.Add(float64(length))
	return nil
}
