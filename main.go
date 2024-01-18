package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	log "github.com/sirupsen/logrus"
)

var (
	verbose                = kingpin.Flag("verbose", "Verbose mode.").Short('v').Bool()
	trace                  = kingpin.Flag("trace", "Trace mode.").Bool()
	proxyAddr              = kingpin.Flag("proxy.listen-addr", "address the proxy will listen on").Required().String()
	nextProxyAddr          = kingpin.Flag("next-proxy.addr", "optional address of another http proxy when cascading usage is required").String()
	metricsAddr            = kingpin.Flag("metrics.listen-addr", "adress the service will listen on for metrics request about itself").String()
	sshUser                = kingpin.Flag("ssh.user", "username used for connecting via ssh").Required().String()
	sshKeyFile             = kingpin.Flag("ssh.key-file", "private key file used for connecting via ssh").Required().String()
	sshKnownHostsFile      = kingpin.Flag("ssh.known-hosts-file", "known hosts file used for connecting via ssh").Required().String()
	sshPort                = kingpin.Flag("ssh.port", "port used for connecting via ssh").Default("22").Int()
	timeout                = kingpin.Flag("timeout", "full roundtrip request timeout in seconds").Default("50").Int()
	timeoutDurationSeconds time.Duration
)

func main() {
	kingpin.Parse()
	timeoutDurationSeconds = time.Duration(*timeout) * time.Second
	if *trace {
		log.SetLevel(log.TraceLevel)
	} else if *verbose {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	log.WithFields(log.Fields{"addr": *proxyAddr}).Info("Listening")
	if *nextProxyAddr != "" {
		log.WithFields(log.Fields{"nextProxyAddr": *nextProxyAddr}).Info("Running in cascading mode: will ssh to nextProxyAddr and use the http proxy there")
	}
	sshTransport, err := NewSSHTransport(*sshUser, *sshKeyFile, *sshKnownHostsFile, *sshPort, *nextProxyAddr)
	if err != nil {
		log.WithFields(log.Fields{"err": err}).Fatal("failed to set up ssh config")
	}
	// only enable HTTPS support at the last sshified instance in a cascading setup:
	enableHTTPS := *nextProxyAddr == ""
	ph := NewProxyHandler(sshTransport, enableHTTPS)
	s := &http.Server{
		Addr:           *proxyAddr,
		Handler:        ph,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   timeoutDurationSeconds,
		MaxHeaderBytes: 1 << 20,
	}

	setupMetrics(*metricsAddr)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for _ = range c {
			log.Info("got SIGHUP, reloading known hosts and key file")
			err := sshTransport.LoadFiles()
			if err == nil {
				log.Info("successfully reloaded")
			} else {
				log.WithFields(log.Fields{"err": err}).Error("reload failed")
			}
		}
	}()
	log.Fatal(s.ListenAndServe())
}
