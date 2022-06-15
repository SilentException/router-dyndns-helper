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

	httpRequestUrl := httpRequest.Url
	httpRequestBody := httpRequest.Body
	if ip.To4() != nil {
		httpRequestUrl = strings.ReplaceAll(httpRequestUrl, ipaddrPlaceholder, ip.String())
		httpRequestBody = strings.ReplaceAll(httpRequestBody, ipaddrPlaceholder, ip.String())
	} else {
		httpRequestUrl = strings.ReplaceAll(httpRequest.Url, ip6addrPlaceholder, ip.String())
		httpRequestBody = strings.ReplaceAll(httpRequestBody, ip6addrPlaceholder, ip.String())
	}
	httpRequestUrlForLog := httpRequestUrl
	httpRequestBodyForLog := httpRequestBody
	for _, usernamePlaceholder := range usernamePlaceholders {
		httpRequestUrl = strings.ReplaceAll(httpRequestUrl, usernamePlaceholder, httpRequest.Username)
		httpRequestBody = strings.ReplaceAll(httpRequestBody, usernamePlaceholder, httpRequest.Username)
	}
	for _, passwordPlaceholder := range passwordPlaceholders {
		httpRequestUrl = strings.ReplaceAll(httpRequestUrl, passwordPlaceholder, httpRequest.Password)
		httpRequestBody = strings.ReplaceAll(httpRequestBody, passwordPlaceholder, httpRequest.Password)
	}

	log.Info(fmt.Sprintf("HTTP request %d: %s %s [%s]", requestIndex, httpRequest.Method, httpRequestUrlForLog, httpRequestBodyForLog))

	go func() {
		request, err := retryablehttp.NewRequest(httpRequest.Method, fmt.Sprintf(httpRequestUrl), bytes.NewBufferString(httpRequestBody))

		if err != nil {
			result <- ResponseResult{requestIndex, "", nil, err}
			return
		}

		if httpRequest.BasicAuth && httpRequest.Username != "" && httpRequest.Password != "" {
			request.SetBasicAuth(httpRequest.Username, httpRequest.Password)
		}

		for requestHeaderKey, requestHeaderValue := range httpRequest.Headers {
			request.Header.Set(requestHeaderKey, requestHeaderValue)
		}

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
