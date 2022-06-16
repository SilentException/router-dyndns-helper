package http_requests

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

var ip4AddrPlaceholders = [...]string{"<ipaddr>", "<ip>", "<ip4>", "<ipv4>", "<ip4addr>", "<ipv4addr>"}
var ip6AddrPlaceholders = [...]string{"<ip6addr>", "<ip6>", "<ipv6>", "<ipv6addr>"}

var usernamePlaceholders = [...]string{"<user>", "<uname>", "<username>"}
var passwordPlaceholders = [...]string{"<pass>", "<passwd>", "<password>"}

type HttpRequest struct {
	Url        string
	Method     string
	Body       string
	Username   string
	Password   string
	BasicAuth  bool
	Timeout    time.Duration
	RetryCount uint
	Onipv4     bool
	Onipv6     bool
	Headers    map[string]string
}

type Updater struct {
	log *log.Entry

	isInit bool

	In chan *net.IP

	Requests []HttpRequest
}

func NewUpdater() *Updater {
	return &Updater{
		log:    log.WithField("module", "http_requests"),
		isInit: false,
		In:     make(chan *net.IP, 10),
	}
}

func (u *Updater) InitFromEnvironment() error {
	//requestIndex = 1
	//for {
	// allows up to 9 custom requests, could go with infinite loop stopping when url is empty but it would be harder to modify existing configuration, this way we allow skipping indexes
	for requestIndex := 1; requestIndex < 10; requestIndex++ {
		// read from HTTP_REQUEST_1_*, HTTP_REQUEST_2_* ... HTTP_REQUEST_9_*, skipping when empty request URL
		httpRequestUrl := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_URL", requestIndex))
		if httpRequestUrl == "" {
			//break
			continue
		}
		httpRequestMethod := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_METHOD", requestIndex))
		if httpRequestMethod == "" {
			httpRequestMethod = "GET"
		}
		httpRequestBody := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_BODY", requestIndex))
		httpRequestUsername := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_USERNAME", requestIndex))
		httpRequestPassword := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_PASSWORD", requestIndex))
		httpRequestBasicAuthStr := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_BASIC_AUTH", requestIndex))
		httpRequestBasicAuth, err := strconv.ParseBool(httpRequestBasicAuthStr)
		if err != nil {
			httpRequestBasicAuth = false
		}
		if httpRequestBasicAuth && (httpRequestUsername == "" || httpRequestPassword == "") {
			httpRequestBasicAuth = false
		}
		httpRequestRetryCountStr := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_RETRY_COUNT", requestIndex))
		// 5 retries is around 30 seconds retry time, each additional retry is extra 30s
		// 7 retries or about a minute and a half retry time seems like a reasonable default and 14 or 5 minutes seems like a reasonable max
		if httpRequestRetryCountStr == "" {
			httpRequestRetryCountStr = "7"
		}
		httpRequestRetryCount, err := strconv.ParseUint(httpRequestRetryCountStr, 10, 32)
		if err != nil || httpRequestRetryCount > 14 {
			if err == nil {
				err = fmt.Errorf("value %d outside bounds [0, 14]", httpRequestRetryCount)
			}
			log.WithError(err).Warn(fmt.Sprintf("Failed to parse HTTP_REQUEST_%d_RETRY_COUNT, using default value 6", requestIndex))
			httpRequestRetryCount = 6
		}
		httpRequestTimeoutStr := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_TIMEOUT", requestIndex))
		if httpRequestTimeoutStr == "" {
			httpRequestTimeoutStr = "5s"
		}
		httpRequestTimeout, err := time.ParseDuration(httpRequestTimeoutStr)
		if err != nil || httpRequestTimeout < time.Duration(1)*time.Second || httpRequestTimeout > time.Duration(60)*time.Second {
			if err == nil {
				err = fmt.Errorf("value %s outside bounds [1s, 60s]", httpRequestTimeout)
			}
			log.WithError(err).Warn(fmt.Sprintf("Failed to parse HTTP_REQUEST_%d_TIMEOUT, using default value 5s", requestIndex))
			httpRequestTimeout = time.Duration(5) * time.Second
		}
		httpRequestOnIpV4Str := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_ONIPV4", requestIndex))
		httpRequestOnIpV4, err := strconv.ParseBool(httpRequestOnIpV4Str)
		if err != nil {
			httpRequestOnIpV4 = true
		}
		httpRequestOnIpV6Str := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_ONIPV6", requestIndex))
		httpRequestOnIpV6, err := strconv.ParseBool(httpRequestOnIpV6Str)
		if err != nil {
			httpRequestOnIpV6 = false
		}
		if httpRequestOnIpV6 {
			httpRequestOnIpV4 = false
		}
		if httpRequestOnIpV4 || httpRequestOnIpV6 {
			httpRequestContainsIp4 := false
			for _, ip4AddrPlaceholder := range ip4AddrPlaceholders {
				httpRequestContainsIp4 = strings.Contains(httpRequestUrl, ip4AddrPlaceholder) || strings.Contains(httpRequestBody, ip4AddrPlaceholder)
			}
			httpRequestContainsIp6 := false
			for _, ip6AddrPlaceholder := range ip6AddrPlaceholders {
				httpRequestContainsIp6 = strings.Contains(httpRequestUrl, ip6AddrPlaceholder) || strings.Contains(httpRequestBody, ip6AddrPlaceholder)
			}
			if httpRequestContainsIp4 {
				httpRequestOnIpV4 = true
				httpRequestOnIpV6 = false
			} else if httpRequestContainsIp6 {
				httpRequestOnIpV4 = false
				httpRequestOnIpV6 = true
			}
		}

		httpRequestHeaders := make(map[string]string)
		for requestHeaderIndex := 1; requestHeaderIndex < 10; requestHeaderIndex++ {
			// read from HTTP_REQUEST_1_HEADER_1_*, HTTP_REQUEST_1_HEADER_1_* ... HTTP_REQUEST_1_HEADER_1_*, skipping when empty header key
			httpRequestHeaderKey := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_HEADER_%d_KEY", requestIndex, requestHeaderIndex))
			if httpRequestHeaderKey == "" {
				//break
				continue
			}
			httpRequestHeaderValue := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_HEADER_%d_VALUE", requestIndex, requestHeaderIndex))
			httpRequestHeaders[httpRequestHeaderKey] = httpRequestHeaderValue
		}

		httpRequest := HttpRequest{httpRequestUrl, httpRequestMethod, httpRequestBody, httpRequestUsername, httpRequestPassword, httpRequestBasicAuth, httpRequestTimeout, uint(httpRequestRetryCount), httpRequestOnIpV4, httpRequestOnIpV6, httpRequestHeaders}

		u.Requests = append(u.Requests, httpRequest)

		//index++
	}

	u.isInit = true

	return nil
}

func (u *Updater) StartWorker() {
	if !u.isInit {
		return
	}

	if len(u.Requests) == 0 {
		return
	}

	go u.spawnWorker()
}

func (u *Updater) spawnWorker() {
	for {
		select {
		case ip := <-u.In:
			u.log.WithField("ip", ip).Info("Received update request, executing all HTTP requests")

			wg := sync.WaitGroup{}

			for i, httpRequest := range u.Requests {
				responseResult := doRequest(httpRequest, i+1, ip, u.log)
				if responseResult == nil {
					continue
				}
				wg.Add(1)
				go func(responseResult chan ResponseResult) {
					defer wg.Done()
					requestResponseResult := <-responseResult
					if requestResponseResult.Error != nil {
						u.log.WithField("http_request_index", requestResponseResult.RequestIndex).
							WithError(requestResponseResult.Error).
							Error("HTTP request failed")
					} else {
						u.log.WithField("http_request_index", requestResponseResult.RequestIndex).
							Info(fmt.Sprintf("HTTP request result: [%s] %s", requestResponseResult.ResponseStatus, string(requestResponseResult.Response)))
					}
				}(responseResult)
			}
			wg.Wait()
		}
	}
}
