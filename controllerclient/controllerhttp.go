package controllerclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/plugin/ochttp/propagation/tracecontext"
	"go.opencensus.io/trace"
)

var logger = logging.Logger("controllerclient")

// http://ip:port/task_tracker/v1/callbackUpload
var ControllerAddress = ""

type ControllerConn struct {
	client *http.Client // client to controller
}

func NewConnector(config *Config) *ControllerConn {
	c := &http.Client{} // timeouts are handled by context timeouts
	c.Transport = &ochttp.Transport{
		Base:           http.DefaultTransport,
		Propagation:    &tracecontext.HTTPFormat{},
		StartOptions:   trace.StartOptions{SpanKind: trace.SpanKindClient},
		FormatSpanName: func(req *http.Request) string { return req.Host + ":" + req.URL.Path + ":" + req.Method },
		NewClientTrace: ochttp.NewSpanAnnotatingClientTrace,
	}

	conn := &ControllerConn{
		client: c,
	}

	logger.Info("ControllerAddress: ", ControllerAddress)
	ControllerAddress = config.Url

	return conn
}

// daemon API.
func (conn *ControllerConn) apiURL() string {
	//return fmt.Sprintf("http://%s/api/v0", ControllerAddress)
	if strings.HasPrefix(ControllerAddress, "http") {
		return ControllerAddress
	}
	return fmt.Sprintf("http://%s", ControllerAddress)
}

func (conn *ControllerConn) doPostCtx(ctx context.Context, apiURL, path, contentType string, postBody io.Reader) (*http.Response, error) {
	logger.Debugf("posting /%s", path)
	urlstr := fmt.Sprintf("%s/%s", apiURL, path)

	req, err := http.NewRequest("POST", urlstr, postBody)
	if err != nil {
		logger.Error("error creating POST request:", err)
		return nil, err
	}

	req.Header.Set("Content-Type", contentType)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	//c := &http.Client{}
	//res, err := c.Do(req)
	res, err := conn.client.Do(req)
	if err != nil {
		logger.Error("error posting to controller:", err)
	}

	return res, err
}

func checkResponse(path string, res *http.Response) ([]byte, error) {
	if res.StatusCode == http.StatusOK {
		return nil, nil
	}

	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err == nil {
		return body, err
	}

	// No error response with useful message from ipfs
	return nil, fmt.Errorf(
		"IPFS request failed (is it running?) (%s). Code %d: %s",
		path,
		res.StatusCode,
		string(body))
}

func (conn *ControllerConn) postCtxStreamResponse(ctx context.Context, path string, contentType string, postBody io.Reader) (io.ReadCloser, error) {
	res, err := conn.doPostCtx(ctx, conn.apiURL(), path, contentType, postBody)
	if err != nil {
		return nil, err
	}

	//_, err = checkResponse(path, res)
	//if err != nil {
	//	return nil, err
	//}
	return res.Body, err
}

// PostCtx post a http request
func (conn *ControllerConn) PostCtx(ctx context.Context, path string, contentType string, postBody io.Reader) ([]byte, error) {
	rdr, err := conn.postCtxStreamResponse(ctx, path, contentType, postBody)
	if err != nil {
		return nil, err
	}
	defer rdr.Close()

	body, err := io.ReadAll(rdr)
	if err != nil {
		logger.Errorf("error reading response body: %s", err)
		return nil, err
	}
	return body, nil
}
