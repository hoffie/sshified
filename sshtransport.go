package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func makePubkeyAuth(keyFile string) ([]ssh.AuthMethod, error) {
	key, err := os.ReadFile(keyFile)
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
	port                   int
	user                   string
	auth                   []ssh.AuthMethod
	sshClientPool          *sshClientPool
	TransportRegular       http.RoundTripper
	TransportTLSSkipVerify http.RoundTripper
	keyFile                string
	knownHostsFile         string
	knownHostsCallback     ssh.HostKeyCallback
	nextProxyAddr          string
}

// trackingSSHClient wraps an ssh.Client and tracks
// all connections opened via DialContext and closed via conn.Close().
// This allows it to implement a safe CloseWhenFinished method,
// which can be used to delay closing of the SSH client until the last
// contained connection has been closed properly.
// This avoids crashing when the SSH connection aborts
// while there's still an inflight HTTP connection over an SSH
// channel.
type trackingSSHClient struct {
	*ssh.Client
	Conn               net.Conn
	mtx                sync.Mutex
	inflightConns      int64
	shouldClose        bool
	keepaliveMtx       sync.Mutex
	keepaliveInflight  bool
	keepaliveWaitChan  chan struct{}
	keepaliveStartTime time.Time
	keepaliveErr       error
}

// trackingSSHConn is a wrapper for net.Conn, which is used by
// trackingSSHClient to ensure that closed connections are properly
// tracked in the client.
type trackingSSHConn struct {
	net.Conn
	closeFunc func()
}

func (conn trackingSSHConn) Close() error {
	err := conn.Conn.Close()
	conn.closeFunc()
	return err
}

func (c *trackingSSHClient) DialContext(ctx context.Context, n, addr string) (net.Conn, error) {
	c.mtx.Lock()
	c.inflightConns++
	c.mtx.Unlock()
	conn, err := c.Client.DialContext(ctx, n, addr)
	if err != nil {
		c.connCloseCallback()
		return conn, err
	}
	tc := trackingSSHConn{Conn: conn, closeFunc: c.connCloseCallback}
	return tc, err
}

func (c *trackingSSHClient) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	// ssh.Client.SendRequest() will cause an infinite loop in its drain logic
	// when it is called on an already closed client.
	// While there may also be internal close calls, the most relevant origin
	// is our code.
	// Therefore, capture our lock, ensure that
	// we haven't called Close() yet and hold the lock
	// until the keepalive returns to avoid racing with parallel
	// KeepAlive calls:
	if c.shouldClose {
		log.Trace("trackingSSHClient: rejecting SendRequest during client shutdown")
		return false, nil, errors.New("trackingSSHClient is shutting down")
	}
	c.mtx.Lock()
	c.inflightConns++
	c.mtx.Unlock()
	// check again, the locking might have taken some time
	if c.shouldClose {
		log.Trace("trackingSSHClient: rejecting SendRequest during client shutdown")
		return false, nil, errors.New("trackingSSHClient is shutting down")
	}
	defer func() {
		c.mtx.Lock()
		c.inflightConns--
		c.mtx.Unlock()
	}()
	return c.Client.SendRequest(name, wantReply, payload)
}

func (c *trackingSSHClient) CheckKeepalive(ctx context.Context) error {
	ctx, ctxCancel := context.WithTimeout(ctx, stepTimeoutDurationSeconds)
	defer ctxCancel()
	c.keepaliveMtx.Lock()
	// we only want one keepalive request inflight; if the previous one did
	// not return yet, this is still relevant for us (connection
	// probably dead)
	if !c.keepaliveInflight {
		c.keepaliveInflight = true
		c.keepaliveWaitChan = make(chan struct{})
		c.keepaliveStartTime = time.Now()
		log.Trace("starting new keepalive goroutine")
		go c.awaitKeepalive()
	}
	c.keepaliveMtx.Unlock()
	if time.Since(c.keepaliveStartTime) > stepTimeoutDurationSeconds {
		log.Trace("previous inflight keepalive already timed out, failing early")
		return errors.New("previous keepalive already timed out")
	}
	log.Trace("waiting for keepalive result")
	select {
	case <-c.keepaliveWaitChan:
		return c.keepaliveErr
	case <-ctx.Done():
		// SendRequest will not have terminated by now and might be
		// waiting for a long time.
		// Sadly, we don't know a reliable and safe way to stop it
		// and will have to live with them being only drained much
		// later.
		return context.Cause(ctx)
	}
}

func (c *trackingSSHClient) awaitKeepalive() {
	log.Trace("awaitKeepalive: SendRequest() start")
	c.Conn.SetDeadline(time.Now().Add(stepTimeoutDurationSeconds))
	defer c.Conn.SetDeadline(time.Time{})
	_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
	log.WithFields(log.Fields{"err": err}).Trace("awaitKeepalive: SendRequest() returned")
	c.keepaliveErr = err
	c.keepaliveInflight = false
	close(c.keepaliveWaitChan)
}

func (c *trackingSSHClient) connCloseCallback() {
	c.mtx.Lock()
	c.inflightConns--
	c.mtx.Unlock()
}

func (c *trackingSSHClient) CloseWhenFinished() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.shouldClose = true
	var err error
	if c.inflightConns <= 0 {
		log.Trace("closing ssh transport connection")
		err = c.Close()
	} else {
		log.WithFields(log.Fields{"inflightConns": c.inflightConns}).Trace("delaying closing of ssh transport connection due to active connections")
	}
	return err
}

func NewSSHTransport(user, keyFile, knownHostsFile string, port int, nextProxyAddr string) (*sshTransport, error) {
	t := &sshTransport{
		port:           port,
		sshClientPool:  newSSHClientPool(),
		keyFile:        keyFile,
		knownHostsFile: knownHostsFile,
		user:           user,
		nextProxyAddr:  nextProxyAddr,
	}
	err := t.LoadFiles()
	if err != nil {
		return nil, err
	}
	t.createTransports()
	return t, nil
}

func (t *sshTransport) LoadFiles() error {
	auth, err := makePubkeyAuth(t.keyFile)
	if err != nil {
		return fmt.Errorf("failed to load private key file: %s", err)
	}
	t.auth = auth
	knownHostsCallback, err := knownhosts.New(t.knownHostsFile)
	if err != nil {
		return fmt.Errorf("failed to load known hosts: %s", err)
	}
	t.knownHostsCallback = knownHostsCallback
	return nil
}

func (t *sshTransport) createTransports() {
	transportRegular := &http.Transport{
		Proxy:                 nil,
		DialContext:           t.dialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       2 * timeoutDurationSeconds,
		ResponseHeaderTimeout: timeoutDurationSeconds,
		ExpectContinueTimeout: 1 * time.Second,
	}
	transportTLSSkipVerify := transportRegular.Clone()
	transportTLSSkipVerify.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	t.TransportRegular = transportRegular
	t.TransportTLSSkipVerify = transportTLSSkipVerify
}

func (t *sshTransport) checkHostKey(hostname string, remote net.Addr, key ssh.PublicKey) error {
	log.WithFields(log.Fields{"hostname": hostname, "remote": remote, "key": key}).Warn("blindly accepting host key")
	return nil
}

func (t *sshTransport) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		log.WithFields(log.Fields{"network": network, "addr": addr}).Error("network type not supported")
		return nil, fmt.Errorf("network type %s is not supported", network)
	}
	if t.nextProxyAddr != "" {
		addr = t.nextProxyAddr
	}
	targetHost, targetPort, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		metricErrorsByType.WithLabelValues("address_parsing").Inc()
		return nil, errors.New("failed to parse address")
	}
	var err error
	for attempt := 1; attempt <= 2; attempt++ {
		var client *trackingSSHClient
		// ensure that err is assigned properly, no := here:
		client, err = t.getSSHClient(targetHost)
		if err != nil {
			metricErrorsByType.WithLabelValues("ssh_connection").Inc()
			return nil, fmt.Errorf("failed to obtain ssh connection: %s", err)
		}
		log.WithFields(log.Fields{"port": targetPort}).Trace("connecting")
		// it's important to choose a smaller timeout here than our caller.
		// otherwise, we might never get a chance to mark the connection as dead,
		// run the keepalive check and force a reconnect:
		dialCtx, dialCancel := context.WithTimeout(ctx, stepTimeoutDurationSeconds)
		conn, err := client.DialContext(dialCtx, "tcp4", net.JoinHostPort("127.0.0.1", targetPort))
		dialCancel()
		log.WithFields(log.Fields{"port": targetPort, "err": err}).Trace("done")
		if err == nil {
			return conn, nil
		}
		log.WithFields(log.Fields{"host": targetHost, "err": err}).Debug("connection failed, sending keepalive")
		// ensure that the request is still valid at all:
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		default:
		}
		keepaliveErr := client.CheckKeepalive(ctx)
		if keepaliveErr == nil {
			log.WithFields(log.Fields{"host": targetHost}).Debug("keepalive worked, this is not an ssh conn problem")
			return nil, err
		}
		metricErrorsByType.WithLabelValues("ssh_keepalive_failure").Inc()
		log.WithFields(log.Fields{"host": targetHost, "err": err, "attempt": attempt}).Debug("keepalive failed")
		t.sshClientPool.delete(targetHost)
		// Don't close right away, there might still be inflight
		// requests which would otherwise crash as they reference
		// invalid memory:
		_ = client.CloseWhenFinished()
		metricSSHKeepaliveFailuresTotal.Inc()
	}
	return nil, err
}

// getHostkeyAlgosFor queries the knownhosts database for the given hostport with an invalid
// key to match against. This generates an error which can be used to query for the
// available key type algorithms.
func (t *sshTransport) getHostkeyAlgosFor(hostport string) ([]string, error) {
	placeholderAddr := &net.TCPAddr{IP: []byte{0, 0, 0, 0}}
	var placeholderPubkey invalidPublicKey
	var algos []string
	var knownHostsLookupError *knownhosts.KeyError
	if err := t.knownHostsCallback(hostport, placeholderAddr, &placeholderPubkey); errors.As(err, &knownHostsLookupError) {
		for _, knownKey := range knownHostsLookupError.Want {
			algos = append(algos, knownKey.Key.Type())
		}
	}
	if len(algos) < 1 {
		metricErrorsByType.WithLabelValues("ssh_host_key_unknown").Inc()
		return []string{}, fmt.Errorf("no matching known hosts entry for %s", hostport)
	}
	return algos, nil
}

func (t *sshTransport) getSSHClient(host string) (*trackingSSHClient, error) {
	host = strings.ToLower(host)
	client, cached := t.sshClientPool.get(host)
	if cached {
		log.WithFields(log.Fields{"host": host}).Trace("using cached ssh connection")
		return client, nil
	}
	sshAddr := net.JoinHostPort(host, strconv.Itoa(t.port))
	knownHostAlgos, err := t.getHostkeyAlgosFor(sshAddr)
	if err != nil {
		return nil, err
	}
	upgradedHostKeyAlgos := upgradeHostKeyAlgos(knownHostAlgos)
	log.WithFields(log.Fields{"host": host, "HostKeyAlgorithms": upgradedHostKeyAlgos}).Trace("building ssh connection")
	clientConfig := &ssh.ClientConfig{
		User:              t.user,
		Auth:              t.auth,
		HostKeyCallback:   t.knownHostsCallback,
		HostKeyAlgorithms: upgradedHostKeyAlgos,
		Timeout:           stepTimeoutDurationSeconds,
	}
	conn, err := net.DialTimeout("tcp", sshAddr, clientConfig.Timeout)
	if err != nil {
		log.WithFields(log.Fields{"host": host, "err": err}).Trace("TCP connection failed")
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, sshAddr, clientConfig)
	if err != nil {
		log.WithFields(log.Fields{"host": host, "err": err}).Trace("SSH connection failed")
		return nil, err
	}
	plainClient := ssh.NewClient(c, chans, reqs)

	log.WithFields(log.Fields{"host": host}).Trace("caching successful ssh connection")
	client = &trackingSSHClient{Client: plainClient, Conn: conn}
	cachedClient, cached := t.sshClientPool.setOrGetCached(host, client)
	if cached {
		// we already checked above and did not have a cached client.
		// however, due to concurrent requests, we may now have one.
		// apparently this is the case here.
		// therefore, we drop our newly created client and use the cached one
		// instead.
		_ = client.Close()
		client = cachedClient
	}
	return client, nil
}

// When reading known_host files we find key types such as ssh-rsa.
// When talking to an SSH server, we need to advertise what keys we
// can handle.
// We should not advertise ssh-rsa here, as it is insecure and deprecated.
// Instead, we should advertise the newer rsa-sha2-* methods
// which work with the same key type.
// Therefore, this function replaces ssh-rsa with rsa-sha2*.
func upgradeHostKeyAlgos(algos []string) []string {
	upgraded := []string{}
	for _, algo := range algos {
		if algo == "ssh-rsa" {
			upgraded = append(upgraded, "rsa-sha2-512")
			upgraded = append(upgraded, "rsa-sha2-256")
			continue
		}
		upgraded = append(upgraded, algo)
	}
	return upgraded
}

type invalidPublicKey struct{}

func (invalidPublicKey) Marshal() []byte {
	return []byte("invalid public key")
}

func (invalidPublicKey) Type() string {
	return "invalid public key"
}

func (invalidPublicKey) Verify(_ []byte, _ *ssh.Signature) error {
	return errors.New("this key is never valid")
}
