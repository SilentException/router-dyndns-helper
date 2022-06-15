package http_requests

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"strings"

	"github.com/hashicorp/go-retryablehttp"
	log "github.com/sirupsen/logrus"
)

type ResponseResult struct {
	RequestIndex   int
	ResponseStatus string
	Response       []byte
	Error          error
}

func doRequest(httpRequest HttpRequest, requestIndex int, ip *net.IP, log *log.Entry) chan ResponseResult {
	result := make(chan ResponseResult)

	if !httpRequest.Onipv4 && !httpRequest.Onipv6 {
		return nil
	}
	if ip.To4() != nil && !httpRequest.Onipv4 {
		return nil
	}
	if ip.To4() == nil && !httpRequest.Onipv6 {
		return nil
	}

	var requestUrl string
	var requestBody string
	if ip.To4() != nil {
		requestUrl = strings.ReplaceAll(httpRequest.Url, ipaddrPlaceholder, ip.String())
		requestBody = strings.ReplaceAll(httpRequest.Body, ipaddrPlaceholder, ip.String())
	} else {
		requestUrl = strings.ReplaceAll(httpRequest.Url, ip6addrPlaceholder, ip.String())
		requestBody = strings.ReplaceAll(httpRequest.Body, ip6addrPlaceholder, ip.String())
	}

	log.Info(fmt.Sprintf("HTTP request %d: %s %s [%s]", requestIndex, httpRequest.Method, requestUrl, requestBody))

	go func() {
		request, err := retryablehttp.NewRequest(httpRequest.Method, fmt.Sprintf(requestUrl), bytes.NewBufferString(requestBody))

		if err != nil {
			result <- ResponseResult{requestIndex, "", nil, err}
			return
		}

		if httpRequest.Username != "" && httpRequest.Password != "" {
			request.SetBasicAuth(httpRequest.Username, httpRequest.Password)
		}

		//request.Header.Set("Content-Type", "text/plain; charset=utf-8;") // TODO SE - allow custom http request headers ?

		client := retryablehttp.NewClient()
		client.Logger = log
		//client.RetryWaitMax = time.Second * 60
		client.RetryMax = 6 // 1 minute or so, should be good enough...
		client.HTTPClient.Timeout = httpRequest.Timeout

		response, err := client.Do(request)

		if err != nil {
			var responseStatus string
			if response != nil {
				responseStatus = response.Status
			}
			result <- ResponseResult{requestIndex, responseStatus, nil, err}
			return
		}

		body, err := ioutil.ReadAll(response.Body)

		if err != nil {
			result <- ResponseResult{requestIndex, response.Status, nil, err}
			return
		}

		result <- ResponseResult{requestIndex, response.Status, body, nil}
	}()

	return result
}
