package main

import (
	"sync"

	log "github.com/sirupsen/logrus"
)

type sshClientPool struct {
	pool map[string]*trackingSshClient
	lock *sync.RWMutex
}

func newSSHClientPool() *sshClientPool {
	p := &sshClientPool{
		pool: make(map[string]*trackingSshClient),
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

func (p *sshClientPool) get(host string) (*trackingSshClient, bool) {
	log.Trace("acquiring cache lock")
	p.lock.RLock()
	defer p.lock.RUnlock()
	client, cached := p.pool[host]
	return client, cached
}

// setOrGetCached checks if a client is already cached for the given
// host. If yes, it returns the cached client.
// If not, it puts the given client into the cache.
func (p *sshClientPool) setOrGetCached(host string, client *trackingSshClient) (*trackingSshClient, bool) {
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
