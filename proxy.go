package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/textparse"
	log "github.com/sirupsen/logrus"
)

type proxyHandler struct {
	ssh         *sshTransport
	enableHTTPS bool
}

func NewProxyHandler(ssh *sshTransport, enableHTTPS bool) *proxyHandler {
	return &proxyHandler{ssh: ssh, enableHTTPS: enableHTTPS}
}

func (ph *proxyHandler) ServeHTTP(rw http.ResponseWriter, origReq *http.Request) {
	proxyReq := NewProxyRequest(rw, origReq, ph.ssh.TransportRegular, ph.ssh.TransportTLSSkipVerify, ph.enableHTTPS)
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
	rw                      http.ResponseWriter
	origReq                 *http.Request
	transportRegular        http.RoundTripper
	transportTLSSkipVerify  http.RoundTripper
	requestedURL            string
	upstreamClient          *http.Client
	upstreamResponse        *http.Response
	upstreamRequest         *http.Request
	enableHTTPS             bool
	httpsInsecureSkipVerify bool
}

func NewProxyRequest(rw http.ResponseWriter, origReq *http.Request, transportRegular, transportTLSSkipVerify http.RoundTripper, enableHTTPS bool) *proxyRequest {
	return &proxyRequest{
		rw:                     rw,
		origReq:                origReq,
		transportRegular:       transportRegular,
		transportTLSSkipVerify: transportTLSSkipVerify,
		enableHTTPS:            enableHTTPS,
	}
}

func (pr *proxyRequest) Handle() error {
	metricRequestsTotal.Inc()
	timer := prometheus.NewTimer(metricRequestDuration)
	defer timer.ObserveDuration()
	pr.prepareHTTPSURL()
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

func (pr *proxyRequest) prepareHTTPSURL() {
	if !pr.enableHTTPS {
		return
	}
	https := pr.origReq.URL.Query().Get("__sshified_use_https")
	if https == "" {
		pr.origReq.URL.Scheme = "http"
	} else {
		pr.origReq.URL.Scheme = "https"
		values := pr.origReq.URL.Query()
		if values.Get("__sshified_https_insecure_skip_verify") == "1" {
			pr.httpsInsecureSkipVerify = true
		}
		for k := range values {
			if strings.HasPrefix(k, "__sshified_") {
				values.Del(k)
			}
		}
		pr.origReq.URL.RawQuery = values.Encode()
	}
}

func (pr *proxyRequest) buildURL() {
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
		metricErrorsByType.WithLabelValues("request_creation").Inc()
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
	var transport http.RoundTripper
	if pr.httpsInsecureSkipVerify {
		transport = pr.transportTLSSkipVerify
	} else {
		transport = pr.transportRegular
	}
	pr.upstreamClient = &http.Client{
		Transport: transport,
		Timeout:   timeoutDurationSeconds,
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
		metricErrorsByType.WithLabelValues("upstream_request").Inc()
		return errors.New("upstream request failed")
	}
	pr.upstreamResponse = upstreamResponse
	return nil
}

func parsableAsPrometheus(b []byte, contentType string) error {
	parser, err := textparse.New(b, contentType, false, labels.NewSymbolTable())
	if err != nil {
		return errors.New("failed to create parser for Prometheus metrics format")
	}
	for {
		_, err := parser.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to parse as Prometheus metrics format: %v", err)
		}
	}
	return nil
}

func (pr *proxyRequest) forwardResponse() error {
	assumeHttpErr := true
	defer func() {
		if assumeHttpErr {
			pr.rw.WriteHeader(http.StatusInternalServerError)
		}
	}()
	respHeader := pr.rw.Header()
	upstreamRespHeader := pr.upstreamResponse.Header
	var reader io.Reader
	if *responseMaxBytes <= 0 {
		reader = pr.upstreamResponse.Body
	} else {
		lr := io.LimitReader(pr.upstreamResponse.Body, *responseMaxBytes)
		var buf = bytes.Buffer{}
		_, err := io.Copy(&buf, lr)
		if err != nil {
			return fmt.Errorf("failed to copy response to buffer: %v", err)
		}
		reader = &buf
		if *responseRejectNonPrometheus {
			log.Trace("parsing response as prometheus metrics")
			var decBytes []byte
			switch upstreamRespHeader.Get("Content-Encoding") {
			case "gzip":
				log.Trace("decoding gzip response")
				bufReader := bytes.NewReader(buf.Bytes())
				gzipReader, err := gzip.NewReader(bufReader)
				if err != nil {
					return fmt.Errorf("failed to create gzip reader: %v", err)
				}
				defer gzipReader.Close()
				gzipLimitReader := io.LimitReader(gzipReader, *responseMaxBytes)
				decBytes, err = ioutil.ReadAll(gzipLimitReader)
				if err != nil {
					return fmt.Errorf("failed to create gzip reader: %v", err)
				}
			default:
				log.WithFields(log.Fields{"headers": upstreamRespHeader}).Trace("headers")
				decBytes = buf.Bytes()
			}
			err := parsableAsPrometheus(decBytes, upstreamRespHeader.Get("Content-Type"))
			if err != nil {
				assumeHttpErr = false
				pr.rw.WriteHeader(http.StatusBadGateway)
				return err
			}
		}
	}

	for k, vv := range pr.upstreamResponse.Header {
		if k == "Content-Length" {
			continue
		}
		for _, v := range vv {
			log.WithFields(log.Fields{"header": k, "value": v}).Trace("copying response header")
			respHeader.Add(k, v)
		}
	}
	assumeHttpErr = false
	pr.rw.WriteHeader(pr.upstreamResponse.StatusCode)
	log.Trace("copying response body")
	length, err := io.Copy(pr.rw, reader)
	if err != nil {
		log.WithFields(log.Fields{"err": err}).Debug("failed to forward response body")
		metricErrorsByType.WithLabelValues("response_body_forwarding").Inc()
		return errors.New("failed to forward response body")
	}
	log.WithFields(log.Fields{"len": length}).Trace("done with copying response body")
	metricPayloadBytes.Add(float64(length))
	return nil
}
