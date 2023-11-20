package registry

import "crypto/tls"

type Option func(*Registry)

func WithTLSCert(cert *tls.Certificate) Option {
	return func(r *Registry) {
		r.cert = cert
	}
}
