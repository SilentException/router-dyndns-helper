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

const ipaddrPlaceholder string = "<ipaddr>"
const ip6addrPlaceholder string = "<ip6addr>"

type HttpRequest struct {
	Url      string
	Method   string
	Body     string
	Username string
	Password string
	Timeout  time.Duration
	Onipv4   bool
	Onipv6   bool
	//Headers string
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
	//index = 1
	//for {
	// allows up to 9 custom requests, could go with infinite loop stopping when url is empty but it would be harder to modify existing configuration, this way we allow skipping indexes
	for index := 1; index < 10; index++ {
		// read from HTTP_REQUEST_1_*, HTTP_REQUEST_2_* ... HTTP_REQUEST_9_*, skipping when empty request URL
		httpRequestUrl := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_URL", index))
		if httpRequestUrl == "" {
			//break
			continue
		}
		httpRequestMethod := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_METHOD", index))
		if httpRequestMethod == "" {
			httpRequestMethod = "GET"
		}
		httpRequestBody := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_BODY", index))
		httpRequestUsername := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_USERNAME", index))
		httpRequestPassword := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_PASSWORD", index))
		httpRequestTimeoutStr := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_TIMEOUT", index))
		if httpRequestTimeoutStr == "" {
			httpRequestTimeoutStr = "5s"
		}
		httpRequestTimeout, err := time.ParseDuration(httpRequestTimeoutStr)
		if err != nil || httpRequestTimeout < time.Duration(1)*time.Second || httpRequestTimeout > time.Duration(60)*time.Second {
			if err == nil {
				err = fmt.Errorf("value %s outside bounds [1s, 60s]", httpRequestTimeout)
			}
			log.WithError(err).Warn(fmt.Sprintf("Failed to parse HTTP_REQUEST_%d_TIMEOUT, using default value 5s", index))
			httpRequestTimeout = time.Duration(5) * time.Second
		}
		httpRequestOnIpV4Str := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_ONIPV4", index))
		httpRequestOnIpV4, err := strconv.ParseBool(httpRequestOnIpV4Str)
		if err != nil {
			httpRequestOnIpV4 = true
		}
		httpRequestOnIpV6Str := os.Getenv(fmt.Sprintf("HTTP_REQUEST_%d_ONIPV6", index))
		httpRequestOnIpV6, err := strconv.ParseBool(httpRequestOnIpV6Str)
		if err != nil {
			httpRequestOnIpV6 = false
		}
		if httpRequestOnIpV6 {
			httpRequestOnIpV4 = false
		}
		if httpRequestOnIpV4 || httpRequestOnIpV6 {
			if strings.Contains(httpRequestUrl, ipaddrPlaceholder) || strings.Contains(httpRequestBody, ipaddrPlaceholder) {
				httpRequestOnIpV4 = true
				httpRequestOnIpV6 = false
			} else if strings.Contains(httpRequestUrl, ip6addrPlaceholder) || strings.Contains(httpRequestBody, ip6addrPlaceholder) {
				httpRequestOnIpV4 = false
				httpRequestOnIpV6 = true
			}
		}

		httpRequest := HttpRequest{httpRequestUrl, httpRequestMethod, httpRequestBody, httpRequestUsername, httpRequestPassword, httpRequestTimeout, httpRequestOnIpV4, httpRequestOnIpV6}

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
				go func(chanResponseResult chan ResponseResult) {
					defer wg.Done()
					requestResponseResult := <-chanResponseResult
					if requestResponseResult.Error != nil {
						u.log.WithError(requestResponseResult.Error).Error(fmt.Sprintf("HTTP request %d failed", requestResponseResult.RequestIndex))
					} else {
						u.log.Info(fmt.Sprintf("HTTP request %d result: [%s] %s", requestResponseResult.RequestIndex, requestResponseResult.ResponseStatus, string(requestResponseResult.Response)))
					}
				}(responseResult)
			}
			wg.Wait()
		}
	}
}
