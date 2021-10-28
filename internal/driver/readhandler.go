package driver

import (
	"context"
	"fmt"

	"github.com/edgexfoundry/device-opcua-go/internal/config"
	sdkModel "github.com/edgexfoundry/device-sdk-go/v2/pkg/models"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/models"
	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

// HandleReadCommands triggers a protocol Read operation for the specified device.
func (d *Driver) HandleReadCommands(deviceName string, protocols map[string]models.ProtocolProperties,
	reqs []sdkModel.CommandRequest) ([]*sdkModel.CommandValue, error) {

	d.Logger.Debugf("Driver.HandleReadCommands: protocols: %v resource: %v attributes: %v", protocols, reqs[0].DeviceResourceName, reqs[0].Attributes)

	// create device client and open connection
	endpoint, err := config.FetchEndpoint(protocols)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()

	client := opcua.NewClient(endpoint, opcua.SecurityMode(ua.MessageSecurityModeNone))
	if err := client.Connect(ctx); err != nil {
		d.Logger.Warnf("Driver.HandleReadCommands: Failed to connect OPCUA client, %s", err)
		return nil, err
	}
	defer client.Close()

	return d.processReadCommands(client, reqs)
}

func (d *Driver) processReadCommands(client *opcua.Client, reqs []sdkModel.CommandRequest) ([]*sdkModel.CommandValue, error) {
	var responses = make([]*sdkModel.CommandValue, len(reqs))

	for i, req := range reqs {
		// handle every reqs
		res, err := d.handleReadCommandRequest(client, req)
		if err != nil {
			d.Logger.Errorf("Driver.HandleReadCommands: Handle read commands failed: %v", err)
			return responses, err
		}
		responses[i] = res
	}

	return responses, nil
}

func (d *Driver) handleReadCommandRequest(deviceClient *opcua.Client, req sdkModel.CommandRequest) (*sdkModel.CommandValue, error) {
	var result = &sdkModel.CommandValue{}
	var err error

	_, isMethod := req.Attributes[METHOD]

	if isMethod {
		result, err = makeMethodCall(deviceClient, req)
		d.Logger.Infof("Method command finished: %v", result)
	} else {
		result, err = makeReadRequest(deviceClient, req)
		d.Logger.Infof("Read command finished: %v", result)
	}

	return result, err
}

func makeReadRequest(deviceClient *opcua.Client, req sdkModel.CommandRequest) (*sdkModel.CommandValue, error) {
	nodeID, err := buildNodeID(req.Attributes, SYMBOL)
	if err != nil {
		return nil, fmt.Errorf("Driver.handleReadCommands: %v", err)
	}

	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("Driver.handleReadCommands: Invalid node id=%s; %v", nodeID, err)
	}

	request := &ua.ReadRequest{
		MaxAge: 2000,
		NodesToRead: []*ua.ReadValueID{
			{NodeID: id},
		},
		TimestampsToReturn: ua.TimestampsToReturnBoth,
	}
	resp, err := deviceClient.Read(request)
	if err != nil {
		return nil, fmt.Errorf("Driver.handleReadCommands: Read failed: %s", err)
	}
	if resp.Results[0].Status != ua.StatusOK {
		return nil, fmt.Errorf("Driver.handleReadCommands: Status not OK: %v", resp.Results[0].Status)
	}

	// make new result
	reading := resp.Results[0].Value.Value()
	return newResult(req, reading)
}

func makeMethodCall(deviceClient *opcua.Client, req sdkModel.CommandRequest) (*sdkModel.CommandValue, error) {
	var inputs []*ua.Variant

	objectID, err := buildNodeID(req.Attributes, OBJECT)
	if err != nil {
		return nil, fmt.Errorf("Driver.handleReadCommands: %v", err)
	}
	oid, err := ua.ParseNodeID(objectID)
	if err != nil {
		return nil, fmt.Errorf("Driver.handleReadCommands: %v", err)
	}

	methodID, err := buildNodeID(req.Attributes, METHOD)
	if err != nil {
		return nil, fmt.Errorf("Driver.handleReadCommands: %v", err)
	}
	mid, err := ua.ParseNodeID(methodID)
	if err != nil {
		return nil, fmt.Errorf("Driver.handleReadCommands: %v", err)
	}

	inputMap, ok := req.Attributes[INPUTMAP]
	if ok {
		imElements := inputMap.([]interface{})
		if len(imElements) > 0 {
			inputs = make([]*ua.Variant, len(imElements))
			for i := 0; i < len(imElements); i++ {
				inputs[i] = ua.MustVariant(imElements[i].(string))
			}
		}
	}

	request := &ua.CallMethodRequest{
		ObjectID:       oid,
		MethodID:       mid,
		InputArguments: inputs,
	}

	resp, err := deviceClient.Call(request)
	if err != nil {
		return nil, fmt.Errorf("Driver.handleReadCommands: Method call failed: %s", err)
	}
	if resp.StatusCode != ua.StatusOK {
		return nil, fmt.Errorf("Driver.handleReadCommands: Method status not OK: %v", resp.StatusCode)
	}

	return newResult(req, resp.OutputArguments[0].Value())
}