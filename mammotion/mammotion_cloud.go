package mammotion

import (
	"container/list"
	"encoding/base64"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	aliyuniot "mammo/aliyuniot"
	mqtt "mammo/data/mqtt"
	pb "mammo/proto"
	"google.golang.org/protobuf/proto"
)

type MammotionCloud struct {
	cloudClient         *aliyuniot.CloudIOTGateway
	isReady             bool
	commandQueue        chan Command
	waitingQueue        *list.List
	mqttMessageEvent    DataEvent
	mqttPropertiesEvent DataEvent
	onReadyEvent        DataEvent
	onDisconnectedEvent DataEvent
	onConnectedEvent    DataEvent
	operationLock       sync.Mutex
	mqttClient          *MammotionMQTT
}

type Command struct {
	iotID   string
	key     string
	command []byte
	future  chan []byte
}

func NewMammotionCloud(mqttClient *MammotionMQTT, cloudClient *aliyuniot.CloudIOTGateway) *MammotionCloud {
	mc := &MammotionCloud{
		cloudClient:         cloudClient,
		commandQueue:        make(chan Command, 100),
		waitingQueue:        list.New(),
		mqttMessageEvent:    NewDataEvent(),
		mqttPropertiesEvent: NewDataEvent(),
		onReadyEvent:        NewDataEvent(),
		onDisconnectedEvent: NewDataEvent(),
		onConnectedEvent:    NewDataEvent(),
		mqttClient:          mqttClient,
	}

	mc.mqttClient.OnConnected = mc.onConnected
	mc.mqttClient.OnDisconnected = mc.onDisconnected
	mc.mqttClient.OnMessage = mc.onMQTTMessage
	mc.mqttClient.OnReady = mc.onReady

	go mc.processQueue()

	return mc
}

func (mc *MammotionCloud) onReady() {
	mc.onReadyEvent.Trigger(nil)
}

func (mc *MammotionCloud) IsConnected() bool {
	return mc.mqttClient.IsConnected
}

func (mc *MammotionCloud) Disconnect() {
	mc.mqttClient.Disconnect()
}

func (mc *MammotionCloud) ConnectAsync() {
	mc.mqttClient.ConnectAsync()
}

func (mc *MammotionCloud) SendCommand(iotID string, command []byte) {
	mc.mqttClient.GetCloudClient().SendCloudCommand(iotID, command)
}

func (mc *MammotionCloud) onConnected() {
	mc.onConnectedEvent.Trigger(nil)
}

func (mc *MammotionCloud) onDisconnected() {
	mc.onDisconnectedEvent.Trigger(nil)
}

func (mc *MammotionCloud) processQueue() {
	for {
		select {
		case cmd := <-mc.commandQueue:
			mc.executeCommandLocked(cmd)
		}
	}
}

func (mc *MammotionCloud) executeCommandLocked(cmd Command) {
	mc.operationLock.Lock()
	defer mc.operationLock.Unlock()

	log.Printf("Sending command: %s", cmd.key)
	mc.mqttClient.GetCloudClient().SendCloudCommand(cmd.iotID, cmd.command)

	future := NewMammotionFuture(cmd.iotID)
	mc.waitingQueue.PushBack(future)

	timeout := time.After(5 * time.Second)
	select {
	case notifyMsg := <-future.Result:
		cmd.future <- notifyMsg
	case <-timeout:
		log.Printf("command_locked TimeoutError")
		cmd.future <- []byte{}
	}
}

func (mc *MammotionCloud) onMQTTMessage(topic string, payload []byte, iotID string) {
	if mc.waitingQueue.Len() > 0 {
		fut := mc.DequeueByIotID(iotID)
		if fut != nil {
			fut.Resolve(payload)
		}
	}

	var payloadMap map[string]interface{}
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		log.Printf("Error unmarshalling payload: %v", err)
		return
	}

	mc.mqttMessageEvent.Trigger(map[string]interface{}{"topic": topic, "payload": payloadMap})
	mc.handleMQTTMessage(topic, payloadMap)
}

func (mc *MammotionCloud) handleMQTTMessage(topic string, payload map[string]interface{}) {
	mc.parseMQTTResponse(topic, payload)
}

func (mc *MammotionCloud) DequeueByIotID(iotID string) *MammotionFuture {
	for e := mc.waitingQueue.Front(); e != nil; e = e.Next() {
		if e.Value.(*MammotionFuture).IotID == iotID {
			mc.waitingQueue.Remove(e)
			return e.Value.(*MammotionFuture)
		}
	}
	return nil
}

func (mc *MammotionCloud) parseMQTTResponse(topic string, payload map[string]interface{}) {
	if strings.HasSuffix(topic, "/app/down/thing/events") {
		// Check the method first to determine how to parse
		method, ok := payload["method"].(string)
		if !ok {
			return
		}

		if method == "thing.properties" {
			// Parse as properties message directly
			var propMsg mqtt.ThingPropertiesMessage
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				log.Printf("Error marshalling properties payload: %v", err)
				return
			}
			err = json.Unmarshal(payloadBytes, &propMsg)
			if err != nil {
				log.Printf("Error parsing properties message: %v", err)
				return
			}
			mc.mqttPropertiesEvent.Trigger(&propMsg)
		} else if method == "thing.events" {
			// Parse as event message - contains protobuf data
			// Extract params.identifier to check if it's a protobuf event
			if params, ok := payload["params"].(map[string]interface{}); ok {
				if identifier, ok := params["identifier"].(string); ok && identifier == "device_protobuf_msg_event" {
					// Extract the base64-encoded protobuf data from params.value.content
					if value, ok := params["value"].(map[string]interface{}); ok {
						if content, ok := value["content"].(string); ok {
							// Decode base64
							decodedData, err := base64.StdEncoding.DecodeString(content)
							if err != nil {
								log.Printf("Error decoding base64 protobuf: %v", err)
								return
							}

							// Parse the protobuf LubaMsg
							var lubaMsg pb.LubaMsg
							err = proto.Unmarshal(decodedData, &lubaMsg)
							if err != nil {
								log.Printf("Error unmarshalling protobuf: %v", err)
								return
							}

							// Check if this is a system message with battery data
							if sysMsg := lubaMsg.GetSys(); sysMsg != nil {
								if reportData := sysMsg.GetToappReportData(); reportData != nil {
									if devStatus := reportData.GetDev(); devStatus != nil {
										batteryLevel := devStatus.GetBatteryVal()
										// Trigger event with battery data
										mc.mqttMessageEvent.Trigger(map[string]interface{}{
											"battery": batteryLevel,
											"device_status": devStatus,
										})
									}
								}
							}
						}
					}
				}
			}
		}
	} else if strings.HasSuffix(topic, "/app/down/thing/properties") {
		// Properties might also come on this topic
		log.Printf("Properties message received on topic: %s", topic)
		var propMsg mqtt.ThingPropertiesMessage
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			log.Printf("Error marshalling properties payload: %v", err)
			return
		}
		err = json.Unmarshal(payloadBytes, &propMsg)
		if err != nil {
			log.Printf("Error parsing properties message: %v", err)
			return
		}
		log.Printf("Properties received for device: %s, battery: %v%%", propMsg.Params.IotID, propMsg.Params.Items.BatteryPercentage.Value)
		mc.mqttPropertiesEvent.Trigger(&propMsg)
	}
}

type DataEvent struct {
	subscribers []func(interface{})
}

func NewDataEvent() DataEvent {
	return DataEvent{
		subscribers: make([]func(interface{}), 0),
	}
}

func (de *DataEvent) AddSubscriber(subscriber func(interface{})) {
	de.subscribers = append(de.subscribers, subscriber)
}

func (de *DataEvent) Trigger(data interface{}) {
	for _, subscriber := range de.subscribers {
		subscriber(data)
	}
}

type MammotionFuture struct {
	IotID  string
	Result chan []byte
}

func NewMammotionFuture(iotID string) *MammotionFuture {
	return &MammotionFuture{
		IotID:  iotID,
		Result: make(chan []byte, 1),
	}
}

func (mf *MammotionFuture) Resolve(result []byte) {
	mf.Result <- result
}



