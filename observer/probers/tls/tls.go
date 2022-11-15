package probers

import (
	"crypto/tls"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

// TLSProbe is the exported 'Prober' object for monitors configured to
// perform TLS protocols.
type TLSProbe struct {
	url        string
	root       string
	response   string
	certExpiry *prometheus.GaugeVec
}

// Name returns a string that uniquely identifies the monitor.
func (p TLSProbe) Name() string {
	return fmt.Sprintf("%s-%s", p.url, p.root)
}

// Kind returns a name that uniquely identifies the `Kind` of `Prober`.
func (p TLSProbe) Kind() string {
	return "TLS"
}

// Probe performs the configured TLS protocol.
// Return true if both root AND response are the expected values, otherewise false
// Export time to cert expiry as Prometheus metric
func (p TLSProbe) Probe(timeout time.Duration) (bool, time.Duration) {
	expected_root, expected_response := false, false
	conf := &tls.Config{
		InsecureSkipVerify: true,
	}
	start := time.Now()
	conn, err := tls.Dial("tcp", p.url, conf)
	if err != nil {
		return false, time.Since(start)
	}
	defer conn.Close()
	chains := conn.ConnectionState().VerifiedChains
	for _, chain := range chains {
		root_cert := chain[len(chain)-1]
		if p.root == fmt.Sprintf("/O=%s/CN=%s", root_cert.Issuer.Organization, root_cert.Issuer.CommonName) {
			expected_root = true
			break
		}
	}
	end_cert := chains[0][0]
	time_to_expiry := time.Until(end_cert.NotAfter)

	if time_to_expiry < 0 {
		expected_response = (p.response == "expired")
	} //TODO: add cases here to check if valid or revoked

	//Report time to expiration (in seconds) for this site
	p.certExpiry.WithLabelValues(p.url).Set(time_to_expiry.Seconds())

	return expected_root && expected_response, time.Since(start)
}
