package main

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/adrianrudnik/fritzbox-cloudflare-dyndns/pkg/avm"
	"github.com/adrianrudnik/fritzbox-cloudflare-dyndns/pkg/cloudflare"
	"github.com/adrianrudnik/fritzbox-cloudflare-dyndns/pkg/dyndns"
	"github.com/adrianrudnik/fritzbox-cloudflare-dyndns/pkg/http_requests"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

type Updaters struct {
	CloudFlare   *cloudflare.Updater
	HttpRequests *http_requests.Updater
	In           chan *net.IP
}

func main() {
	// Load any env variables defined in .env.dev files
	_ = godotenv.Load(".env", ".env.dev")

	initLog()

	ipv6LocalAddress := os.Getenv("DEVICE_LOCAL_ADDRESS_IPV6")
	var localIp net.IP
	if ipv6LocalAddress != "" {
		localIp = net.ParseIP(ipv6LocalAddress)
		if localIp == nil {
			log.Error("Failed to parse IP from DEVICE_LOCAL_ADDRESS_IPV6, exiting")
			return
		}
		log.Info("Using the IPv6 prefix to construct the IPv6 address")
	}

	updaters := createAndStartUpdaters()
	go spawnUpdateWorker(updaters)

	startPollServer(updaters.In, &localIp)
	startPushServer(updaters.In, &localIp)

	shutdown := make(chan os.Signal)

	signal.Notify(shutdown, syscall.SIGTERM)
	signal.Notify(shutdown, syscall.SIGINT)

	<-shutdown

	log.Info("Shutdown detected")
}

func initLog() {
	// log timestamps & set log level
	log.SetFormatter(&log.TextFormatter{TimestampFormat: "2006-01-02 15:04:05", FullTimestamp: true})
	logLevel, err := log.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		log.WithError(err).Warn("Failed to parse log level from LOG_LEVEL, using default INFO")
		logLevel = log.InfoLevel
	}
	log.SetLevel(logLevel)
}

func newFritzBox() *avm.FritzBox {
	fb := avm.NewFritzBox()

	// Import FritzBox endpoint url
	endpointUrl := os.Getenv("FRITZBOX_ENDPOINT_URL")

	if endpointUrl != "" {
		v, err := url.ParseRequestURI(endpointUrl)

		if err != nil {
			log.WithError(err).Panic("Failed to parse env FRITZBOX_ENDPOINT_URL")
		}

		fb.Url = strings.TrimRight(v.String(), "/")
	} else {
		log.Info("Env FRITZBOX_ENDPOINT_URL not found, disabling FritzBox polling")
		return nil
	}

	// Import FritzBox endpoint timeout setting
	endpointTimeout := os.Getenv("FRITZBOX_ENDPOINT_TIMEOUT")

	if endpointTimeout != "" {
		v, err := time.ParseDuration(endpointTimeout)

		if err != nil {
			log.WithError(err).Warn("Failed to parse FRITZBOX_ENDPOINT_TIMEOUT, using defaults")
		} else {
			fb.Timeout = v
		}
	}

	return fb
}

func createAndStartUpdaters() *Updaters {
	CloudFlareUpdater := newCloudFlareUpdater()
	CloudFlareUpdater.StartWorker()

	HttpRequestsUpdater := newHttpRequestsUpdater()
	HttpRequestsUpdater.StartWorker()

	return &Updaters{
		CloudFlare:   CloudFlareUpdater,
		HttpRequests: HttpRequestsUpdater,
		In:           make(chan *net.IP, 10),
	}
}

func spawnUpdateWorker(updaters *Updaters) {
	for {
		select {
		case ip := <-updaters.In:
			log.WithField("ip", ip).Info("Received update request, sending to all updaters")
			updaters.CloudFlare.In <- ip
			updaters.HttpRequests.In <- ip
		}
	}
}

func newCloudFlareUpdater() *cloudflare.Updater {
	u := cloudflare.NewUpdater()

	token := os.Getenv("CLOUDFLARE_API_TOKEN")
	email := os.Getenv("CLOUDFLARE_API_EMAIL")
	key := os.Getenv("CLOUDFLARE_API_KEY")

	if token == "" {
		if email == "" || key == "" {
			log.Info("Env CLOUDFLARE_API_TOKEN or CLOUDFLARE_API_EMAIL/CLOUDFLARE_API_KEY not found, disabling CloudFlare updates")
			return u
		} else {
			log.Warn("Using deprecated credentials via the API key")
		}
	}

	ipv4Zone := os.Getenv("CLOUDFLARE_ZONES_IPV4")
	ipv6Zone := os.Getenv("CLOUDFLARE_ZONES_IPV6")

	if ipv4Zone == "" && ipv6Zone == "" {
		log.Warn("Env CLOUDFLARE_ZONES_IPV4 and CLOUDFLARE_ZONES_IPV6 not found, disabling CloudFlare updates")
		return u
	}

	if ipv4Zone != "" {
		u.SetIPv4Zones(ipv4Zone)
	}

	if ipv6Zone != "" {
		u.SetIPv6Zones(ipv6Zone)
	}

	var err error

	if token != "" {
		err = u.InitWithToken(token)
	} else {
		err = u.InitWithKey(email, key)
	}

	if err != nil {
		log.WithError(err).Error("Failed to init Cloudflare updater, disabling CloudFlare updates")
		return u
	}

	return u
}

func newHttpRequestsUpdater() *http_requests.Updater {
	u := http_requests.NewUpdater()

	err := u.InitFromEnvironment()

	if err != nil {
		log.WithError(err).Error("Failed to init HTTP requests updater, disabling HTTP request updates")
		return u
	}

	return u
}

func startPushServer(out chan<- *net.IP, localIp *net.IP) {
	bind := os.Getenv("DYNDNS_SERVER_BIND")

	if bind == "" {
		log.Info("Env DYNDNS_SERVER_BIND not found, disabling DynDns server")
		return
	}

	server := dyndns.NewServer(out, localIp)
	server.Username = os.Getenv("DYNDNS_SERVER_USERNAME")
	server.Password = os.Getenv("DYNDNS_SERVER_PASSWORD")

	serverBasicAuth, err := strconv.ParseBool(os.Getenv("DYNDNS_SERVER_BASIC_AUTH"))
	if err != nil {
		serverBasicAuth = false
	}
	if serverBasicAuth && (server.Username == "" || server.Password == "") {
		serverBasicAuth = false
	}
	server.BasicAuth = serverBasicAuth

	s := &http.Server{
		Addr: bind,
	}

	http.HandleFunc("/ip", server.Handler)

	go func() {
		log.Fatal(s.ListenAndServe())
	}()
}

func startPollServer(out chan<- *net.IP, localIp *net.IP) {
	fritzbox := newFritzBox()

	if fritzbox == nil {
		return
	}

	// Import endpoint polling interval duration
	interval := os.Getenv("FRITZBOX_ENDPOINT_INTERVAL")

	var ticker *time.Ticker

	if interval != "" {
		v, err := time.ParseDuration(interval)

		if err != nil {
			log.WithError(err).Warn("Failed to parse FRITZBOX_ENDPOINT_INTERVAL, using defaults")
			ticker = time.NewTicker(300 * time.Second)
		} else {
			ticker = time.NewTicker(v)
		}
	} else {
		log.Info("Env FRITZBOX_ENDPOINT_INTERVAL not found, disabling polling")
		return
	}

	go func() {
		lastV4 := net.IP{}
		lastV6 := net.IP{}

		poll := func() {
			log.Debug("Polling WAN IPs from router")

			ipv4, err := fritzbox.GetWanIpv4()

			if err != nil {
				log.WithError(err).Warn("Failed to poll WAN IPv4 from router")
			} else {
				if !lastV4.Equal(ipv4) {
					log.WithField("ipv4", ipv4).Info("New WAN IPv4 found")
					out <- &ipv4
					lastV4 = ipv4
				}

			}

			if *localIp == nil {
				ipv6, err := fritzbox.GetwanIpv6()

				if err != nil {
					log.WithError(err).Warn("Failed to poll WAN IPv6 from router")
				} else {
					if !lastV6.Equal(ipv6) {
						log.WithField("ipv6", ipv6).Info("New WAN IPv6 found")
						out <- &ipv6
						lastV6 = ipv6
					}
				}
			} else {
				prefix, err := fritzbox.GetIpv6Prefix()

				if err != nil {
					log.WithError(err).Warn("Failed to poll IPv6 Prefix from router")
				} else {
					if !lastV6.Equal(prefix.IP) {

						constructedIp := make(net.IP, net.IPv6len)
						copy(constructedIp, prefix.IP)

						for i := 0; i < net.IPv6len; i++ {
							constructedIp[i] = constructedIp[i] + (*localIp)[i]
						}

						log.WithField("prefix", prefix).WithField("ipv6", constructedIp).Info("New IPv6 Prefix found")

						out <- &constructedIp
						lastV6 = prefix.IP
					}
				}
			}
		}

		poll()

		for {
			select {
			case <-ticker.C:
				poll()
			}
		}
	}()
}
