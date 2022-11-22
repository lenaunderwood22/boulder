package probers

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/crypto/ocsp"
)

type reason int

const (
	none reason = iota
	internalError
	issuerVerifyFailed
	ocspUnknown
	ocspError
)

var reasonToString = map[reason]string{
	none:               "nil",
	internalError:      "internalError",
	issuerVerifyFailed: "issuerVerifyFailed",
	ocspUnknown:        "ocspUnknown",
	ocspError:          "ocspError",
}

func getReasons() []string {
	var allReasons []string
	for _, v := range reasonToString {
		allReasons = append(allReasons, v)
	}
	return allReasons
}

// TLSProbe is the exported `Prober` object for monitors configured to perform
// TLS protocols.
type TLSProbe struct {
	url      string
	rootOrg  string
	rootCN   string
	response string
	notAfter *prometheus.GaugeVec
	reason   *prometheus.CounterVec
}

// Name returns a string that uniquely identifies the monitor.
func (p TLSProbe) Name() string {
	return p.url
}

// Kind returns a name that uniquely identifies the `Kind` of `Prober`.
func (p TLSProbe) Kind() string {
	return "TLS"
}

// Get OCSP status (good, revoked or unknown) of certificate
func getOCSP(cert, issuer *x509.Certificate) (int, error) {
	req, err := ocsp.CreateRequest(cert, issuer, nil)
	if err != nil {
		return ocsp.Unknown, err
	}

	url := fmt.Sprintf("%s/%s", cert.OCSPServer[0], base64.StdEncoding.EncodeToString(req))
	res, err := http.Get(url)
	if err != nil {
		return ocsp.Unknown, err
	}

	output, err := io.ReadAll(res.Body)
	if err != nil {
		return ocsp.Unknown, err
	}

	ocspRes, err := ocsp.ParseResponseForCert(output, cert, issuer)
	if err != nil {
		return ocsp.Unknown, err
	}

	return ocspRes.Status, nil
}

// Export expiration timestamp and reason to Prometheus.
func (p TLSProbe) exportMetrics(notAfter time.Time, reason reason) {
	p.notAfter.WithLabelValues(p.url).Set(float64(notAfter.Unix()))
	p.notAfter.WithLabelValues(p.url, reasonToString[reason]).Inc()
}

func (p TLSProbe) probeExpired(timeout time.Duration) (bool, time.Duration) {
	start := time.Now()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", p.url+":443", &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		p.exportMetrics(time.Time{}, internalError)
		return false, time.Since(start)
	}
	defer conn.Close()
	peers := conn.ConnectionState().PeerCertificates
	for i := 0; i < len(peers)-1; i++ {
		certIssuer, issuerSubject := peers[i].Issuer, peers[i+1].Subject
		if (certIssuer.CommonName != issuerSubject.CommonName) || (certIssuer.Organization[0] != issuerSubject.Organization[0]) {
			p.exportMetrics(peers[0].NotAfter, issuerVerifyFailed)
			return false, time.Since(start)
		}
	}
	p.exportMetrics(peers[0].NotAfter, none)
	if time.Until(peers[0].NotAfter) > 0 {
		return false, time.Since(start)
	}
	root := peers[len(peers)-1].Issuer
	return p.rootOrg == root.Organization[0] && p.rootCN == root.CommonName, time.Since(start)

}

func (p TLSProbe) probeUnexpired(timeout time.Duration, valid bool) (bool, time.Duration) {
	start := time.Now()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", p.url+":443", &tls.Config{})
	if err != nil {
		p.exportMetrics(time.Time{}, internalError)
		return false, time.Since(start)
	}
	defer conn.Close()
	peers := conn.ConnectionState().PeerCertificates
	root := peers[len(peers)-1].Issuer
	if root.Organization[0] != p.rootOrg || root.CommonName != p.rootCN {
		p.exportMetrics(peers[0].NotAfter, none)
		return false, time.Since(start)
	}

	ocspStatus, err := getOCSP(peers[0], peers[1])
	if err != nil {
		p.exportMetrics(peers[0].NotAfter, ocspError)
		return false, time.Since(start)
	}

	switch ocspStatus {
	case ocsp.Good:
		p.exportMetrics(peers[0].NotAfter, none)
		return valid, time.Since(start)
	case ocsp.Revoked:
		p.exportMetrics(peers[0].NotAfter, none)
		return !valid, time.Since(start)
	default:
		p.exportMetrics(peers[0].NotAfter, ocspUnknown)
		return false, time.Since(start)
	}
}

// Probe performs the configured TLS protocol. Return true if both root AND
// response are the expected values, otherwise false. Export expiration
// timestamp and reason as Prometheus metrics.
func (p TLSProbe) Probe(timeout time.Duration) (bool, time.Duration) {
	switch p.response {
	case "valid":
		return p.probeUnexpired(timeout, true)
	case "revoked":
		return p.probeUnexpired(timeout, false)
	case "expired":
		return p.probeExpired(timeout)
	default:
		return false, 0
	}
}
