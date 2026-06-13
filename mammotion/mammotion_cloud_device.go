package mammotion

import (
	"encoding/base64"
	"fmt"
	"log"
	"sync"
	"time"

	mqtt "mammo/data/mqtt"
	pb "mammo/proto"
	"google.golang.org/protobuf/proto"
)

type MammotionBaseCloudDevice struct {
	mqtt                *MammotionCloud
	device              *MowingDevice
	stateManager        *StateManager
	bleSyncTask         *time.Timer
	stopped             bool
	onReadyCallback     func() error
	commandFutures      map[string]chan []byte
	commands            *MammotionCommand
	currentID           string
	operationLock       sync.Mutex
}

func NewMammotionBaseCloudDevice(mqtt *MammotionCloud, device *MowingDevice, stateManager *StateManager) *MammotionBaseCloudDevice {
	mbcd := &MammotionBaseCloudDevice{
		mqtt:           mqtt,
		device:         device,
		stateManager:   stateManager,
		commandFutures: make(map[string]chan []byte),
		commands:       NewMammotionCommand(device.iotDevice.DeviceName),
	}

	device.mqttMessageEvent.AddSubscriber(mbcd.parseMessageForDevice)
	mqtt.mqttPropertiesEvent.AddSubscriber(mbcd.parseMessagePropertiesForDevice)
	mqtt.onReadyEvent.AddSubscriber(mbcd.onReady)
	mqtt.onDisconnectedEvent.AddSubscriber(mbcd.onDisconnect)
	mqtt.onConnectedEvent.AddSubscriber(mbcd.onConnect)

	if mqtt.isReady {
		mbcd.runPeriodicSyncTask()
	}

	return mbcd
}

func (mbcd *MammotionBaseCloudDevice) onReady(data interface{}) {
	if mbcd.stopped {
		return
	}
	if mbcd.onReadyCallback != nil {
		err := mbcd.onReadyCallback()
		if err != nil {
			log.Printf("Device is offline: %v", err)
		}
	}
}

func (mbcd *MammotionBaseCloudDevice) onDisconnect(data interface{}) {
	if mbcd.bleSyncTask != nil {
		mbcd.bleSyncTask.Stop()
	}
	mbcd.mqtt.Disconnect()
}

func (mbcd *MammotionBaseCloudDevice) onConnect(data interface{}) {
	mbcd.bleSync()
	if mbcd.bleSyncTask == nil || mbcd.bleSyncTask.Stop() {
		mbcd.runPeriodicSyncTask()
	}
}

func (mbcd *MammotionBaseCloudDevice) Stop() {
	if mbcd.bleSyncTask != nil {
		mbcd.bleSyncTask.Stop()
	}
	mbcd.stopped = true
}

func (mbcd *MammotionBaseCloudDevice) Start() {
	mbcd.bleSync()
	if mbcd.bleSyncTask == nil || mbcd.bleSyncTask.Stop() {
		mbcd.runPeriodicSyncTask()
	}
	mbcd.stopped = false
	if !mbcd.mqtt.IsConnected() {
		mbcd.mqtt.ConnectAsync()
	}
}

func (mbcd *MammotionBaseCloudDevice) bleSync() {
	commandBytes := mbcd.commands.SendToDevBleSync(3)
	mbcd.mqtt.SendCommand(mbcd.device.iotDevice.IotId, commandBytes)
}

func (mbcd *MammotionBaseCloudDevice) runPeriodicSyncTask() {
	if !mbcd.operationLock.TryLock() || !mbcd.stopped {
		mbcd.bleSync()
	}
	if !mbcd.stopped {
		mbcd.scheduleBleSync()
	}
}

func (mbcd *MammotionBaseCloudDevice) scheduleBleSync() {
	if mbcd.mqtt != nil && mbcd.mqtt.IsConnected() {
		mbcd.bleSyncTask = time.AfterFunc(160*time.Second, func() {
			mbcd.runPeriodicSyncTask()
		})
	}
}

func (mbcd *MammotionBaseCloudDevice) QueueCommand(key string, kwargs map[string]interface{}) ([]byte, error) {
	future := make(chan []byte)
	commandBytes := mbcd.commands.GetCommandBytes(key, kwargs)
	mbcd.mqtt.commandQueue <- Command{
		iotID:   mbcd.device.iotDevice.IotId,
		key:     key,
		command: commandBytes,
		future:  future,
	}
	select {
	case result := <-future:
		return result, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("command timeout")
	}
}

func (mbcd *MammotionBaseCloudDevice) parseMessageForDevice(event interface{}) {
	thingEventMessage, ok := event.(*mqtt.ThingEventMessage)
	if !ok {
		log.Printf("Failed to cast event to *mqtt.ThingEventMessage")
		return
	}

	var iotID, deviceName, productKey string
	var valueContent string

	switch params := thingEventMessage.Params.(type) {
	case mqtt.DeviceProtobufMsgEventParams:
		iotID = params.IotId
		deviceName = params.DeviceName
		productKey = params.ProductKey
		valueContent = params.Value.Content
	default:
		// try to get general params
		if generalParams, ok := thingEventMessage.Params.(mqtt.GeneralParams); ok {
			iotID = generalParams.IotId
			deviceName = generalParams.DeviceName
			productKey = generalParams.ProductKey
			if val, ok := generalParams.Value.(map[string]interface{}); ok {
				if content, ok := val["content"].(string); ok {
					valueContent = content
				}
			}
		} else {
			log.Printf("Unknown event params type: %T", thingEventMessage.Params)
			return
		}
	}

	if iotID != mbcd.device.iotDevice.IotId {
		return
	}

	binaryData, err := base64.StdEncoding.DecodeString(valueContent)
	if err != nil {
		log.Printf("Error decoding message: %v", err)
		return
	}
	mbcd.updateRawData(binaryData)

	// Parse the protobuf message
	var lubaMsg pb.LubaMsg
	err = proto.Unmarshal(binaryData, &lubaMsg)
	if err != nil {
		log.Printf("Error parsing protobuf message: %v", err)
		return
	}

	// Extract battery data and position from system messages
	if sys := lubaMsg.GetSys(); sys != nil {
		if reportData := sys.GetToappReportData(); reportData != nil {
			// Extract battery level + full dev status (for diagnostics)
			if devStatus := reportData.GetDev(); devStatus != nil {
				batteryLevel := devStatus.GetBatteryVal()
				log.Printf("DEBUG: DevStatus sys_status=%d charge_state=%d battery=%d sensor=%d last_status=%d vslam=%d",
					devStatus.GetSysStatus(), devStatus.GetChargeState(), batteryLevel,
					devStatus.GetSensorStatus(), devStatus.GetLastStatus(), devStatus.GetVslamStatus())
				if lock := devStatus.GetLockState(); lock != nil {
					log.Printf("DEBUG: LockState %+v", lock)
				}
				mbcd.stateManager.UpdateBatteryFromProtobuf(batteryLevel)
				if mbcd.stateManager.OnDeviceStatus != nil {
					mbcd.stateManager.OnDeviceStatus(devStatus.GetSysStatus(), devStatus.GetChargeState())
				}
			}
			if workState := reportData.GetWork(); workState != nil {
				log.Printf("DEBUG: WorkState %+v", workState)
			}
			if rtk := reportData.GetRtk(); rtk != nil {
				log.Printf("DEBUG: RTK %+v", rtk)
			}

			// Extract position data
			if locations := reportData.GetLocations(); len(locations) > 0 {
				// Use the first location (most recent)
				loc := locations[0]
				x := float32(loc.GetRealPosX())
				y := float32(loc.GetRealPosY())
				angle := loc.GetRealToward()
				posType := loc.GetPosType()

				log.Printf("DEBUG: Position update - X=%.0f Y=%.0f Angle=%d PosType=%d", x, y, angle, posType)

				if mbcd.stateManager.OnPositionUpdate != nil {
					mbcd.stateManager.OnPositionUpdate(x, y, angle, posType)
				}
			}
		}
	}

	// Extract navigation data
	if nav := lubaMsg.GetNav(); nav != nil {
		// Extract hash list response
		if hashListAck := nav.GetToappGethashAck(); hashListAck != nil {
			hashListData := &HashListData{
				Hashes: hashListAck.GetDataCouple(),
			}

			if mbcd.stateManager.OnHashListReceived != nil {
				mbcd.stateManager.OnHashListReceived(hashListData)
			}
		}

		// Extract common data response (map data)
		if commonDataAck := nav.GetToappGetCommondataAck(); commonDataAck != nil {
			mapData := &MapData{
				Type:         commonDataAck.GetType(),
				Hash:         int64(commonDataAck.GetHash()),
				DataCouple:   extractDataCouple(commonDataAck.GetDataCouple()),
				TotalFrame:   commonDataAck.GetTotalFrame(),
				CurrentFrame: commonDataAck.GetCurrentFrame(),
				Action:       commonDataAck.GetAction(),
				DataLen:      commonDataAck.GetDataLen(),
				AreaLabel:    commonDataAck.GetAreaLabel().GetLabel(),
			}

			if mbcd.stateManager.OnMapDataReceived != nil {
				mbcd.stateManager.OnMapDataReceived(mapData)
			}
		}

		// Extract charge pile (dock) position
		if chgPile := nav.GetToappChgpileto(); chgPile != nil {
			if mbcd.stateManager.OnChargePilePosition != nil {
				mbcd.stateManager.OnChargePilePosition(chgPile.GetToward(), chgPile.GetX(), chgPile.GetY())
			}
		}

		// Extract planned coverage path (zigzag) frames
		if zz := nav.GetToappZigzag(); zz != nil {
			log.Printf("DEBUG: ZigZag frame job=%d zone=%d/%d frame=%d/%d points=%d",
				zz.GetJobId(), zz.GetCurrentZone(), zz.GetTotalZoneNum(),
				zz.GetCurrentFrame(), zz.GetTotalFrame(), len(zz.GetDataCouple()))
			if mbcd.stateManager.OnZigZagReceived != nil {
				mbcd.stateManager.OnZigZagReceived(&ZigZagData{
					JobId:        zz.GetJobId(),
					CurrentZone:  zz.GetCurrentZone(),
					TotalZoneNum: zz.GetTotalZoneNum(),
					CurrentFrame: zz.GetCurrentFrame(),
					TotalFrame:   zz.GetTotalFrame(),
					CurrentHash:  zz.GetCurrentHash(),
					DataCouple:   extractDataCouple(zz.GetDataCouple()),
					SubCmd:       zz.GetSubCmd(),
				})
			}
		}
	}

	if mbcd.commands.GetDeviceProductKey() == "" && mbcd.commands.GetDeviceName() == deviceName {
		mbcd.commands.SetDeviceProductKey(productKey)
	}
	if mbcd.mqtt.waitingQueue.Len() > 0 {
		fut := mbcd.mqtt.DequeueByIotID(mbcd.device.iotDevice.IotId)
		if fut != nil {
			fut.Resolve(binaryData)
		}
	}

	// Still call the placeholder Notification for compatibility
	newMsg := LubaMsg{}
	newMsg.Parse(binaryData)
	mbcd.stateManager.Notification(&newMsg)
}

func (mbcd *MammotionBaseCloudDevice) parseMessagePropertiesForDevice(event interface{}) {
	thingPropertiesMessage, ok := event.(*mqtt.ThingPropertiesMessage)
	if !ok {
		log.Printf("Failed to cast event to *mqtt.ThingPropertiesMessage")
		return
	}
	if thingPropertiesMessage.Params.IotID != mbcd.device.iotDevice.IotId {
		return
	}
	mbcd.stateManager.Properties(thingPropertiesMessage)
}

func (mbcd *MammotionBaseCloudDevice) updateRawData(data []byte) {
	// Implement this method
}

// extractDataCouple flattens CommDataCouple pairs preserving float precision.
// The device sends coordinates as float32 meters; truncating to int32 (as the
// old code did) collapsed the map to whole-meter resolution.
func extractDataCouple(dataCouples []*pb.CommDataCouple) []float32 {
	result := make([]float32, 0, len(dataCouples)*2)
	for _, dc := range dataCouples {
		result = append(result, dc.GetX(), dc.GetY())
	}
	return result
}
