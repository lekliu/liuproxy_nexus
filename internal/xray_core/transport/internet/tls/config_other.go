//go:build !windows
// +build !windows

package tls

import (
	"crypto/x509"
	"liuproxy_nexus/internal/xray_core/common/errors"
	"sync"
)

type rootCertsCache struct {
	sync.Mutex
	pool *x509.CertPool
}

func (c *rootCertsCache) load() (*x509.CertPool, error) {
	c.Lock()
	defer c.Unlock()

	if c.pool != nil {
		return c.pool, nil
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	c.pool = pool
	return pool, nil
}

var rootCerts rootCertsCache

func (c *Config) getCertPool() (*x509.CertPool, error) {
	if c.DisableSystemRoot {
		return c.loadSelfCertPool()
	}

	if len(c.Certificate) == 0 {
		return rootCerts.load()
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.NewError("system root").Base(err)
	}
	for _, cert := range c.Certificate {
		if !pool.AppendCertsFromPEM(cert.Certificate) {
			return nil, errors.NewError("append cert to root").Base(err)
		}
	}
	return pool, err
}
