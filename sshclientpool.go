package main

import (
	"sync"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

type sshClientPool struct {
	pool map[string]*ssh.Client
	lock *sync.RWMutex
}

func newSSHClientPool() *sshClientPool {
	p := &sshClientPool{
		pool: make(map[string]*ssh.Client),
		lock: &sync.RWMutex{},
	}
	return p
}

func (p *sshClientPool) delete(host string) {
	p.lock.Lock()
	delete(p.pool, host)
	metricSshclientPool.Dec()
	p.lock.Unlock()
}

func (p *sshClientPool) get(host string) (*ssh.Client, bool) {
	log.Debug("acquiring cache lock")
	p.lock.RLock()
	defer p.lock.RUnlock()
	client, cached := p.pool[host]
	return client, cached
}

// setOrGetCached checks if a client is already cached for the given
// host. If yes, it returns the cached client.
// If not, it puts the given client into the cache.
func (p *sshClientPool) setOrGetCached(host string, client *ssh.Client) (*ssh.Client, bool) {
	p.lock.Lock()
	defer p.lock.Unlock()
	cachedClient, cached := p.pool[host]
	if cached {
		return cachedClient, cached
	}
	p.pool[host] = client
	metricSshclientPool.Inc()
	return nil, false
}
