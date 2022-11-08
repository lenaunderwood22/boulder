package probers

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/letsencrypt/boulder/observer/probers"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v3"
)

const (
	certExpiryName = "obs_cert_expiry"
)

// TLSConf is exported to receive YAML configuration.
type TLSConf struct {
	URL        string `yaml:"url"`
	Root       string `yaml:"root"`
	Response   string `yaml:"response"`
}

// Kind returns a name that uniquely identifies the `Kind` of `Configurer`.
func (c TLSConf) Kind() string {
	return "TLS"
}

// UnmarshalSettings takes YAML as bytes and unmarshals it to the to an
// TLSConf object.
func (c TLSConf) UnmarshalSettings(settings []byte) (probers.Configurer, error) {
	var conf TLSConf
	err := yaml.Unmarshal(settings, &conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}

func (c TLSConf) validateURL() error {
	url, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf(
			"invalid 'url', got: %q, expected a valid url", c.URL)
	}
	if url.Scheme == "" {
		return fmt.Errorf(
			"invalid 'url', got: %q, missing scheme", c.URL)
	}
	return nil
}

func (c TLSConf) validateRoot() error {
	// expected example: /O=Internet Security Research Group/CN=ISRG Root X1
	regex, err := regexp.Compile("^/O=[^/]*/CN=[^/]*$")
	if err != nil {
		return err
	}
	matched := regex.MatchString(c.Root)
	if matched {
		return nil
	} else {
		return fmt.Errorf(
			"invalid 'root`, got: %s\nDid not match expected format: '/CN:*/O:*`", c.Root)
	}
}

func (c TLSConf) validateResponse() error {
	acceptable := []string{"valid", "expired", "revoked"}
	for _, a := range acceptable {
		if strings.ToLower(c.Response) == a {
			return nil
		}
	}
	return fmt.Errorf(
		"invalid `response`, got: %s\nMust be one of 'valid', 'expired' or 'revoked'.", c.Response)

}

// MakeProber constructs a `TLSProbe` object from the contents of the
// bound `TLSConf` object. If the `TLSConf` cannot be validated, an
// error appropriate for end-user consumption is returned instead.
func (c TLSConf) MakeProber(collectors map[string]prometheus.Collector) (probers.Prober, error) {
	// validate `url`
	err := c.validateURL()
	if err != nil {
		return nil, err
	}

	// validate `root`
	err = c.validateRoot()
	if err != nil {
		return nil, err
	}

	err = c.validateResponse()
	if err != nil {
		return nil, err
	}

	coll, ok := collectors[certExpiryName]
	if !ok {
		return nil, fmt.Errorf("tls prober did not receive collector %q", certExpiryName)
	}
	certExpiryColl, ok := coll.(*prometheus.GaugeVec)
	if !ok {
		return nil, fmt.Errorf("tls prober received collector %q of wrong type, got: %T, expected *prometheus.GaugeVec", certExpiryName, coll)
	}

	return TLSProbe{c.URL, c.Root, strings.ToLower(c.Response), certExpiryColl}, nil
}

// Instrument is a no-op to implement the `Configurer` interface.
func (c TLSConf) Instrument() map[string]prometheus.Collector {
	certExpiry := prometheus.Collector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: certExpiryName,
			Help: "Time to cert expiry in seconds",
		}, []string{"url"},
	))
	return map[string]prometheus.Collector{
		certExpiryName: certExpiry,
	}
}

// init is called at runtime and registers `HTTPConf`, a `Prober`
// `Configurer` type, as "HTTP".
func init() {
	probers.Register(TLSConf{})
}
