package http_requests

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

type ResponseResult struct {
	RequestIndex   int
	ResponseStatus string
	Response       []byte
	Error          error
}

type RequestLogger struct {
	log                   *log.Entry
	httpRequestIndex      int
	httpRequestUrl        string
	httpRequestBody       string
	httpRequestUrlForLog  string
	httpRequestBodyForLog string
}

func (requestLogger RequestLogger) prepareMessageForLog(logMessage string) string {
	logMessage = strings.ReplaceAll(logMessage, requestLogger.httpRequestUrl, requestLogger.httpRequestUrlForLog)
	//logMessage = strings.ReplaceAll(logMessage, requestLogger.httpRequestBody, requestLogger.httpRequestBodyForLog) // not really useful in this context and might produce incorrect logs
	return logMessage
}

func (requestLogger RequestLogger) prepareErrorForLog(logError error) error {
	return fmt.Errorf(requestLogger.prepareMessageForLog(logError.Error()))
}

func (requestLogger RequestLogger) Printf(message string, args ...interface{}) {
	if requestLogger.log == nil {
		return
	}
	logMessage := requestLogger.prepareMessageForLog(fmt.Sprintf(message, args...))

	if strings.HasPrefix(logMessage, "[ERR") {
		requestLogger.log.Error(logMessage)
	} else {
		requestLogger.log.Debug(logMessage)
	}
}

func (requestLogger RequestLogger) LogRequest(logger retryablehttp.Logger, httpRequest *http.Request, retryNumber int) {
	dumpBytes, err := httputil.DumpRequestOut(httpRequest, true)
	if err != nil {
		requestLogger.log.WithError(err).Error("Dumping request for log failed")
		return
	}
	dumpString := string(dumpBytes)
	requestLogger.log.Trace(dumpString)
}

func (requestLogger RequestLogger) LogResponse(logger retryablehttp.Logger, httpResponse *http.Response) {
	if httpResponse.Request != nil && httpResponse.Status != "" {
		desc := fmt.Sprintf("%s %s", httpResponse.Request.Method, requestLogger.prepareMessageForLog(fmt.Sprintf("%s", httpResponse.Request.URL)))
		requestLogger.log.Debug(fmt.Sprintf("[DEBUG] %s response status: %s", desc, httpResponse.Status))
	}

	dumpBytes, err := httputil.DumpResponse(httpResponse, true)
	if err != nil {
		requestLogger.log.WithError(err).Error("Dumping response for log failed")
		return
	}
	dumpString := string(dumpBytes)
	requestLogger.log.Trace(dumpString)
}

func doRequest(httpRequest HttpRequest, requestIndex int, ip *net.IP, log *log.Entry) chan ResponseResult {
	responseResult := make(chan ResponseResult)

	if !httpRequest.Onipv4 && !httpRequest.Onipv6 {
		return nil
	}
	if ip.To4() != nil && !httpRequest.Onipv4 {
		return nil
	}
	if ip.To4() == nil && !httpRequest.Onipv6 {
		return nil
	}

	if ip.To4() != nil {
		for _, ip4AddrPlaceholder := range ip4AddrPlaceholders {
			httpRequest.Url = strings.ReplaceAll(httpRequest.Url, ip4AddrPlaceholder, ip.String())
			httpRequest.Body = strings.ReplaceAll(httpRequest.Body, ip4AddrPlaceholder, ip.String())
		}
	} else {
		for _, ip6AddrPlaceholder := range ip6AddrPlaceholders {
			httpRequest.Url = strings.ReplaceAll(httpRequest.Url, ip6AddrPlaceholder, ip.String())
			httpRequest.Body = strings.ReplaceAll(httpRequest.Body, ip6AddrPlaceholder, ip.String())
		}
	}
	httpRequestUrlForLog := httpRequest.Url
	httpRequestBodyForLog := httpRequest.Body
	for _, usernamePlaceholder := range usernamePlaceholders {
		httpRequest.Url = strings.ReplaceAll(httpRequest.Url, usernamePlaceholder, httpRequest.Username)
		httpRequest.Body = strings.ReplaceAll(httpRequest.Body, usernamePlaceholder, httpRequest.Username)
	}
	for _, passwordPlaceholder := range passwordPlaceholders {
		httpRequest.Url = strings.ReplaceAll(httpRequest.Url, passwordPlaceholder, httpRequest.Password)
		httpRequest.Body = strings.ReplaceAll(httpRequest.Body, passwordPlaceholder, httpRequest.Password)
	}

	log.WithField("http_request_index", requestIndex).Info(fmt.Sprintf("HTTP request: %s %s [%s]", httpRequest.Method, httpRequestUrlForLog, httpRequestBodyForLog))
	requestLogger := &RequestLogger{
		log:                   log.WithFields(logrus.Fields{"submodule": "retryablehttp", "http_request_index": requestIndex}),
		httpRequestIndex:      requestIndex,
		httpRequestUrl:        httpRequest.Url,
		httpRequestBody:       httpRequest.Body,
		httpRequestUrlForLog:  httpRequestUrlForLog,
		httpRequestBodyForLog: httpRequestBodyForLog,
	}

	go func(httpRequest HttpRequest, requestLogger RequestLogger, responseResult chan ResponseResult) {
		request, err := retryablehttp.NewRequest(httpRequest.Method, httpRequest.Url, bytes.NewBufferString(httpRequest.Body))

		if err != nil {
			responseResult <- ResponseResult{requestIndex, "", nil, requestLogger.prepareErrorForLog(err)}
			return
		}

		if httpRequest.BasicAuth && httpRequest.Username != "" && httpRequest.Password != "" {
			request.SetBasicAuth(httpRequest.Username, httpRequest.Password)
		}

		for requestHeaderKey, requestHeaderValue := range httpRequest.Headers {
			request.Header.Set(requestHeaderKey, requestHeaderValue)
		}

		client := retryablehttp.NewClient()
		client.Logger = requestLogger
		client.RequestLogHook = requestLogger.LogRequest
		client.ResponseLogHook = requestLogger.LogResponse
		client.RetryWaitMax = time.Second * 30
		client.RetryMax = int(httpRequest.RetryCount)
		client.HTTPClient.Timeout = httpRequest.Timeout

		response, err := client.Do(request)

		if err != nil {
			var responseStatus string
			if response != nil {
				responseStatus = response.Status
			}
			responseResult <- ResponseResult{requestIndex, responseStatus, nil, requestLogger.prepareErrorForLog(err)}
			return
		}

		body, err := ioutil.ReadAll(response.Body)

		if err != nil {
			responseResult <- ResponseResult{requestIndex, response.Status, nil, requestLogger.prepareErrorForLog(err)}
			return
		}

		responseResult <- ResponseResult{requestIndex, response.Status, body, nil}
	}(httpRequest, *requestLogger, responseResult)

	return responseResult
}
