package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func splitAddr(addr string) (host string, port int, err error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		err = errors.New("invalid address format")
		return
	}
	host = parts[0]
	port, err = strconv.Atoi(parts[1])
	if err != nil {
		err = errors.New("invalid port number")
		return
	}
	return
}

func makePubkeyAuth(keyFile string) ([]ssh.AuthMethod, error) {
	key, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read private key file %s", keyFile)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("unable to parse private key file %s", keyFile)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil

}

type sshTransport struct {
	port            int
	user            string
	clientConfig    *ssh.ClientConfig
	clientCache     map[string]*ssh.Client
	clientCacheLock *sync.RWMutex
	Transport       http.RoundTripper
	keyFile         string
	knownHostsFile  string
}

func NewSSHTransport(user, keyFile, knownHostsFile string, port int) (*sshTransport, error) {
	t := &sshTransport{
		port:            port,
		clientCache:     make(map[string]*ssh.Client),
		clientCacheLock: &sync.RWMutex{},
		keyFile:         keyFile,
		knownHostsFile:  knownHostsFile,
		user:            user,
	}
	t.LoadFiles()
	t.createTransport()
	return t, nil
}

func (t *sshTransport) LoadFiles() error {
	auth, err := makePubkeyAuth(t.keyFile)
	if err != nil {
		return fmt.Errorf("failed to load private key file: %s", err)
	}
	knownHosts, err := knownhosts.New(t.knownHostsFile)
	if err != nil {
		return fmt.Errorf("failed to load known hosts: %s", err)
	}
	t.clientConfig = &ssh.ClientConfig{
		User:            t.user,
		Auth:            auth,
		HostKeyCallback: knownHosts,
		Timeout:         5 * time.Second,
	}
	return nil
}

func (t *sshTransport) createTransport() {
	t.Transport = &http.Transport{
		Proxy: nil,
		Dial:  t.dial,
		// FIXME: DialContext
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (t *sshTransport) checkHostKey(hostname string, remote net.Addr, key ssh.PublicKey) error {
	log.WithFields(log.Fields{"hostname": hostname, "remote": remote, "key": key}).Warn("blindly accepting host key")
	return nil
}

func (t *sshTransport) dial(network, addr string) (net.Conn, error) {
	if network != "tcp" {
		log.WithFields(log.Fields{"network": network, "addr": addr}).Error("network type not supported")
		return nil, fmt.Errorf("network type %s is not supported", network)
	}
	targetHost, targetPort, err := splitAddr(addr)
	if err != nil {
		return nil, errors.New("failed to parse address")
	}
	retry := true
	for retry {
		client, err := t.getSSHClient(targetHost)
		if err != nil {
			return nil, fmt.Errorf("failed to obtain ssh connection: %s", err)
		}
		log.WithFields(log.Fields{"port": targetPort}).Debug("connecting")
		conn, err := client.Dial("tcp4", fmt.Sprintf("%s:%d", "127.0.0.1", targetPort))
		log.WithFields(log.Fields{"port": targetPort, "err": err}).Debug("done")
		if err != nil {
			// connection failed? may be caused by lost ssh transport
			// connection; let's assume it is dead and try again once with a fresh one.
			log.WithFields(log.Fields{"host": targetHost, "err": err}).Warn("connection failed, retrying with new connection")
			t.invalidateClientCacheFor(targetHost)
			retry = false
			continue
		}
		return conn, err
	}
	return nil, err
}

func (t *sshTransport) invalidateClientCacheFor(host string) {
	t.clientCacheLock.Lock()
	delete(t.clientCache, host)
	t.clientCacheLock.Unlock()
}

func (t *sshTransport) getSSHClient(host string) (*ssh.Client, error) {
	log.Debug("acquiring cache lock")
	t.clientCacheLock.RLock()
	client, cached := t.clientCache[host]
	t.clientCacheLock.RUnlock()
	if cached {
		log.WithFields(log.Fields{"host": host}).Debug("using cached ssh connection")
		return client, nil
	}
	log.WithFields(log.Fields{"host": host}).Debug("building ssh connection")
	sshAddr := fmt.Sprintf("%s:%d", host, t.port)
	client, err := ssh.Dial("tcp", sshAddr, t.clientConfig)
	if err == nil {
		log.WithFields(log.Fields{"host": host}).Debug("caching successful ssh connection")
		t.clientCacheLock.Lock()
		cachedClient, cached := t.clientCache[host]
		if cached {
			// we already checked above and did not have a cached client.
			// however, due to concurrent requests, we may now have one.
			// apparently this is the case here.
			// therefore, we drop our newly created client and use the cached one
			// instead.
			client.Close()
			client = cachedClient
		} else {
			t.clientCache[host] = client
		}
		t.clientCacheLock.Unlock()
	}
	return client, err
}
