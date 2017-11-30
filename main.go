package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	verbose           = kingpin.Flag("verbose", "Verbose mode.").Short('v').Bool()
	proxyAddr         = kingpin.Flag("proxy.listen-addr", "address the proxy will listen on").Required().String()
	sshUser           = kingpin.Flag("ssh.user", "username used for connecting via ssh").Required().String()
	sshKeyFile        = kingpin.Flag("ssh.key-file", "private key file used for connecting via ssh").Required().String()
	sshKnownHostsFile = kingpin.Flag("ssh.known-hosts-file", "known hosts file used for connecting via ssh").Required().String()
	sshPort           = kingpin.Flag("ssh.port", "port used for connecting via ssh").Default("22").Int()
)

func main() {
	kingpin.Parse()
	if *verbose {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	log.WithFields(log.Fields{"addr": *proxyAddr}).Info("Listening")
	sshTransport, err := NewSSHTransport(*sshUser, *sshKeyFile, *sshKnownHostsFile, *sshPort)
	if err != nil {
		log.WithFields(log.Fields{"err": err}).Fatal("failed to set up ssh config")
	}
	ph := NewProxyHandler(sshTransport)
	s := &http.Server{
		Addr:           *proxyAddr,
		Handler:        ph,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
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
