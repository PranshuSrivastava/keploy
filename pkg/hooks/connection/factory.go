package connection

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"go.keploy.io/server/pkg"
	"go.keploy.io/server/pkg/hooks/structs"
	"go.keploy.io/server/pkg/models"
	"go.keploy.io/server/pkg/models/spec"
	"go.keploy.io/server/pkg/platform"
	"go.uber.org/zap"
)

// Factory is a routine-safe container that holds a trackers with unique ID, and able to create new tracker.
type Factory struct {
	connections         map[structs.ConnID]*Tracker
	inactivityThreshold time.Duration
	mutex               *sync.RWMutex
	logger *zap.Logger
}

// NewFactory creates a new instance of the factory.
func NewFactory(inactivityThreshold time.Duration, logger *zap.Logger) *Factory {
	return &Factory{
		connections:         make(map[structs.ConnID]*Tracker),
		mutex:               &sync.RWMutex{},
		inactivityThreshold: inactivityThreshold,
		logger: logger,
	}
}
// func (factory *Factory) HandleReadyConnections(k *keploy.Keploy) {
func (factory *Factory) HandleReadyConnections(db platform.TestCaseDB, getDeps func() []*models.Mock, resetDeps func() int) {

	trackersToDelete := make(map[structs.ConnID]struct{})
	for connID, tracker := range factory.connections {
		if tracker.IsComplete() {
			trackersToDelete[connID] = struct{}{}
			if len(tracker.sentBuf) == 0 && len(tracker.recvBuf) == 0 {
				continue
			}

			parsedHttpReq, err1 := ParseHTTPRequest(tracker.recvBuf)
			parsedHttpRes, err2 := ParseHTTPResponse(tracker.sentBuf, parsedHttpReq)
			if err1 != nil {
				factory.logger.Error("failed to parse the http request from byte array", zap.Error(err1))
				continue
			}
			if err2 != nil {
				factory.logger.Error("failed to parse the http response from byte array", zap.Error(err2))
				continue
			}

			// capture the ingress call for record cmd
			if models.GetMode() == models.MODE_RECORD {
				capture(db, parsedHttpReq, parsedHttpRes, getDeps, factory.logger)
				resetDeps()
			}
		} else if tracker.Malformed() {
			trackersToDelete[connID] = struct{}{}
		} else if tracker.IsInactive(factory.inactivityThreshold) {
			trackersToDelete[connID] = struct{}{}
		}
	}
	factory.mutex.Lock()
	defer factory.mutex.Unlock()
	for key := range trackersToDelete {
		delete(factory.connections, key)
	}
}

// GetOrCreate returns a tracker that related to the given connection and transaction ids. If there is no such tracker
// we create a new one.
func (factory *Factory) GetOrCreate(connectionID structs.ConnID) *Tracker {
	factory.mutex.Lock()
	defer factory.mutex.Unlock()
	tracker, ok := factory.connections[connectionID]
	if !ok {
		factory.connections[connectionID] = NewTracker(connectionID, factory.logger)
		return factory.connections[connectionID]
	}
	return tracker
}

func capture(db platform.TestCaseDB, req *http.Request, resp *http.Response, getDeps func() []*models.Mock, logger *zap.Logger) {
	meta := map[string]string{
		"method": req.Method,
	}
	httpMock := &models.Mock{
		Version: models.V1Beta2,
		Name:    "",
		Kind:    models.HTTP,
	}

	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		logger.Error("failed to read the http request body", zap.Error(err))
		return
	}
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read the http response body", zap.Error(err))
		return
	}
	

	// encode the message into yaml
	err = httpMock.Spec.Encode(&spec.HttpSpec{
			Metadata: meta,
			Request: spec.HttpReqYaml{
				Method:     spec.Method(req.Method),
				ProtoMajor: req.ProtoMajor,
				ProtoMinor: req.ProtoMinor,
				URL:        req.URL.String(),
				Header:     pkg.ToYamlHttpHeader(req.Header),
				Body:       string(reqBody),
				URLParams: 	pkg.UrlParams(req),
			},
			Response: spec.HttpRespYaml{
				StatusCode: resp.StatusCode,
				Header:     pkg.ToYamlHttpHeader(resp.Header),
				Body: string(respBody),
			},
			Created: time.Now().Unix(),
			Assertions: make(map[string][]string),
			Mocks: []string{},
	})
	if err != nil {
		logger.Error("failed to encode http spec for testcase", zap.Error(err))
		return
	}

	// write yaml
	err = db.Insert(httpMock, getDeps())
	if err!=nil {
		logger.Error("failed to record the ingress requests", zap.Error(err))
		return
	}
}

func ParseHTTPRequest(requestBytes []byte) (*http.Request, error) {

	// Parse the request using the http.ReadRequest function
	request, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(requestBytes)))
	if err != nil {
		return nil, err
	}

	return request, nil
}

func ParseHTTPResponse(data []byte, request *http.Request) (*http.Response, error) {
	buffer := bytes.NewBuffer(data)
	reader := bufio.NewReader(buffer)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, err
	}
	return response, nil
}