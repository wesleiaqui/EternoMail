package app

import (
	"github.com/hkdb/aerion/internal/certificate"
	"github.com/hkdb/aerion/internal/logging"
)

// ============================================================================
// Certificate Trust API - Exposed to frontend via Wails bindings
// ============================================================================

// AcceptCertificate accepts a certificate for the given host.
// If permanent is true, the certificate is stored in the database.
// If permanent is false, the certificate is only trusted for the current session.
func (a *App) AcceptCertificate(host string, info certificate.CertificateInfo, permanent bool) error {
	log := logging.WithComponent("app.certificate")

	if permanent {
		log.Info().
			Str("host", host).
			Str("fingerprint", info.Fingerprint).
			Msg("Permanently accepting certificate")
		return a.certStore.AcceptPermanently(host, &info)
	}

	log.Info().
		Str("host", host).
		Str("fingerprint", info.Fingerprint).
		Msg("Accepting certificate for session")
	return a.certStore.AcceptSession(host, info.Fingerprint)
}

// GetTrustedCertificates returns permanently trusted certificates for the given hosts
func (a *App) GetTrustedCertificates(hosts []string) ([]*certificate.CertificateInfo, error) {
	return a.certStore.GetByHosts(hosts)
}

// RemoveTrustedCertificate removes a certificate from the trust store by fingerprint
func (a *App) RemoveTrustedCertificate(fingerprint string) error {
	log := logging.WithComponent("app.certificate")
	log.Info().Str("fingerprint", fingerprint).Msg("Removing trusted certificate")
	return a.certStore.Remove(fingerprint)
}
