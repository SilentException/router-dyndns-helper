package dyndns

import (
	"crypto/subtle"
	"net"
	"net/http"

	log "github.com/sirupsen/logrus"
)

type Server struct {
	log     *log.Entry
	out     chan<- *net.IP
	localIp *net.IP

	Username  string
	Password  string
	BasicAuth bool
}

func NewServer(out chan<- *net.IP, localIp *net.IP) *Server {
	return &Server{
		log:     log.WithField("module", "dyndns"),
		out:     out,
		localIp: localIp,
	}
}

// Handler offers a simple HTTP handler func for an HTTP server.
// It expects the IP address parameters and will relay them towards the CloudFlare updater
// worker once they get submitted.
//
// Expected parameters can be
//   "ipaddr" IPv4 address
//   "ip6addr" IPv6 address
//
// see https://service.avm.de/help/de/FRITZ-Box-Fon-WLAN-7490/016/hilfe_dyndns
func (s *Server) Handler(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	s.log.Info("Received incoming DynDNS update")

	// check request basic auth, if configured
	username, password, ok := r.BasicAuth()
	if s.BasicAuth && !ok {
		s.log.Warn("Rejected due to basic auth mismatch")
		w.Header().Set("WWW-Authenticate", "Basic realm=\"Authentication required to access this resource\"")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// use request parameters for auth, if basic auth is not configured
	if !s.BasicAuth {
		username = params.Get("username")
		password = params.Get("password")
	}

	// check username / password match
	if subtle.ConstantTimeCompare([]byte(username), []byte(s.Username)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(s.Password)) != 1 {
		s.log.Warn("Rejected due to username / password mismatch")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse IPv4
	ipv4 := net.ParseIP(params.Get("v4"))
	if ipv4 != nil && ipv4.To4() != nil {
		s.log.WithField("ipv4", ipv4).Info("Forwarding update request for IPv4")
		s.out <- &ipv4
	}

	if *s.localIp == nil {
		// Parse IPv6
		ipv6 := net.ParseIP(params.Get("v6"))
		if ipv6 != nil && ipv6.To4() == nil {
			s.log.WithField("ipv6", ipv6).Info("Forwarding update request for IPv6")
			s.out <- &ipv6
		}
	} else {
		// Parse Prefix
		_, prefix, err := net.ParseCIDR(params.Get("prefix"))
		if err != nil {
			s.log.WithError(err).Warn("Failed to parse prefix")
		} else {
			constructedIp := make(net.IP, net.IPv6len)
			copy(constructedIp, prefix.IP)

			for i := 0; i < net.IPv6len; i++ {
				constructedIp[i] = constructedIp[i] + (*s.localIp)[i]
			}

			s.log.WithField("prefix", prefix).WithField("ipv6", constructedIp).Info("Forwarding update request for IPv6")
			s.out <- &constructedIp
		}
	}

	w.WriteHeader(200)
}
