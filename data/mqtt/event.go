package mqtt

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Define the event types
type DeviceProtobufMsgEventParams struct {
	GeneralParams
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
	Value      struct {
		Content string `json:"content"`
	} `json:"value"`
}

type DeviceWarningEventParams struct {
	GeneralParams
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
	Value      struct {
		Code int `json:"code"`
	} `json:"value"`
}

type DeviceNotificationEventParams struct {
	GeneralParams
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
	Value      struct {
		Data string `json:"data"`
	} `json:"value"`
}

type DeviceBizReqEventParams struct {
	GeneralParams
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
	Value      struct {
		BizType string `json:"bizType"`
		BizId   string `json:"bizId"`
		Params  string `json:"params"`
	} `json:"value"`
}

type DeviceConfigurationRequestEvent struct {
	GeneralParams
	Type  string `json:"type"`
	Value struct {
		Code  int    `json:"code"`
		BizId string `json:"bizId"`
		Params string `json:"params"`
	} `json:"value"`
}

type ThingEventMessage struct {
	Method  string      `json:"method"`
	Id      string      `json:"id"`
	Params  interface{} `json:"params"`
	Version string      `json:"version"`
	Type string 
}

// GeneralParams is a placeholder for common parameters
type GeneralParams struct {
	GroupIdList       []string `json:"groupIdList"`
	GroupId           string   `json:"groupId"`
	CategoryKey       string   `json:"categoryKey"`
	BatchId           string   `json:"batchId"`
	GmtCreate         int      `json:"gmtCreate"`
	ProductKey        string   `json:"productKey"`
	Type              string   `json:"type"`
	DeviceName        string   `json:"deviceName"`
	IotId             string   `json:"iotId"`
	CheckLevel        int      `json:"checkLevel"`
	Namespace         string   `json:"namespace"`
	TenantId          string   `json:"tenantId"`
	Name              string   `json:"name"`
	ThingType         string   `json:"thingType"`
	Time              int      `json:"time"`
	TenantInstanceId  string   `json:"tenantInstanceId"`
	Value             interface{} `json:"value"`
	Identifier        *string  `json:"identifier,omitempty"`
	CheckFailedData   *map[string]interface{} `json:"checkFailedData,omitempty"`
	TenantIdOptional  *string  `json:"_tenantId,omitempty"`
	GenerateTime      *int     `json:"generateTime,omitempty"`
	JMSXDeliveryCount *int     `json:"JMSXDeliveryCount,omitempty"`
	Qos               *int     `json:"qos,omitempty"`
	RequestId         *string  `json:"requestId,omitempty"`
	CategoryKeyOptional *string `json:"_categoryKey,omitempty"`
	DeviceType        *string  `json:"deviceType,omitempty"`
	TraceId           *string  `json:"_traceId,omitempty"`
}

// FromJSON deserializes the payload JSON into a ThingEventMessage
func FromJSON(payload []byte) (*ThingEventMessage, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}

    return FromMap(raw)
}

func FromMap(raw map[string]interface{}) (*ThingEventMessage, error) {
	method, ok := raw["method"].(string)
	if !ok {
		return nil, errors.New("missing or invalid method")
	}

	eventID, ok := raw["id"].(string)
	if !ok {
		return nil, errors.New("missing or invalid id")
	}

	version, ok := raw["version"].(string)
	if !ok {
		return nil, errors.New("missing or invalid version")
	}

	params, ok := raw["params"].(map[string]interface{})
	if !ok {
		return nil, errors.New("missing or invalid params")
	}

	identifier, ok := params["identifier"].(string)
	if !ok {
		return nil, errors.New("missing or invalid identifier")
	}

	var paramsObj interface{}
	switch identifier {
	case "device_protobuf_msg_event":
		var event DeviceProtobufMsgEventParams
		if err := mapToStruct(params, &event); err != nil {
			return nil, err
		}
		paramsObj = event
	case "device_warning_event":
		var event DeviceWarningEventParams
		if err := mapToStruct(params, &event); err != nil {
			return nil, err
		}
		paramsObj = event
	case "device_notification_event", "device_warning_code_event":
		var event DeviceNotificationEventParams
		if err := mapToStruct(params, &event); err != nil {
			return nil, err
		}
		paramsObj = event
	case "device_biz_req_event":
		var event DeviceBizReqEventParams
		if err := mapToStruct(params, &event); err != nil {
			return nil, err
		}
		paramsObj = event
	case "device_config_req_event":
		var event DeviceConfigurationRequestEvent
		if err := mapToStruct(params, &event); err != nil {
			return nil, err
		}
		paramsObj = event
	default:
		return nil, fmt.Errorf("unknown identifier: %s", identifier)
	}

	return &ThingEventMessage{
		Method:  method,
		Id:      eventID,
		Params:  paramsObj,
		Version: version,
        Type: identifier,
	}, nil
}

// mapToStruct maps a map to a struct
func mapToStruct(m map[string]interface{}, s interface{}) error {
	bytes, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, s)
}
