package data

import (
    "sync"
    "time"
    "mammo/mammotion"
    "mammo/data/model"
    "mammo/data/mqtt"
)

type StateManager struct {
    Device                *mammotion.MowingDevice
    LastUpdatedAt         time.Time
    GetHashAckCallback    func(*model.NavGetHashListAck)
    GetCommonDataAckCallback func(interface{})
    OnNotificationCallback func(string, interface{})
    QueueCommandCallback  func(string, map[string]interface{}) ([]byte, error)
    mu                    sync.Mutex
}

func NewStateManager(device *mammotion.MowingDevice) *StateManager {
    return &StateManager{
        Device:        device,
        LastUpdatedAt: time.Now(),
    }
}

func (sm *StateManager) GetDevice() *mammotion.MowingDevice {
    return sm.Device
}

func (sm *StateManager) SetDevice(device *mammotion.MowingDevice) {
    sm.Device = device
}

func (sm *StateManager) Properties(properties *mqtt.ThingPropertiesMessage) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    sm.Device.MQTTProperties = properties.Params
}

func (sm *StateManager) Notification(message *model.LubaMsg) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    sm.LastUpdatedAt = time.Now()

    switch message.Type {
    case "nav":
        sm.updateNavData(message)
    case "sys":
        sm.updateSysData(message)
    case "driver":
        sm.updateDriverData(message)
    case "net":
        sm.updateNetData(message)
    case "mul":
        sm.updateMulData(message)
    case "ota":
        sm.updateOtaData(message)
    }

    if sm.OnNotificationCallback != nil {
        sm.OnNotificationCallback(message.Type, message)
    }
}

func (sm *StateManager) updateNavData(message *model.LubaMsg) {
    switch message.Nav.Type {
    case "toapp_gethash_ack":
        hashlistAck := message.Nav.GetHashAck
        sm.Device.Map.UpdateRootHashList(hashlistAck)
        if sm.GetHashAckCallback != nil {
            sm.GetHashAckCallback(hashlistAck)
        }
    case "toapp_get_commondata_ack":
        commonData := message.Nav.GetCommonDataAck
        updated := sm.Device.Map.Update(commonData)
        if updated && sm.GetCommonDataAckCallback != nil {
            sm.GetCommonDataAckCallback(commonData)
        }
    case "toapp_svg_msg":
        commonData := message.Nav.SvgMessageAck
        updated := sm.Device.Map.Update(commonData)
        if updated && sm.GetCommonDataAckCallback != nil {
            sm.GetCommonDataAckCallback(commonData)
        }
    case "toapp_all_hash_name":
        hashNames := message.Nav.GetAllAreaHashName
        convertedList := make([]model.AreaHashNameList, len(hashNames.HashNames))
        for i, item := range hashNames.HashNames {
            convertedList[i] = model.AreaHashNameList{Name: item.Name, Hash: item.Hash}
        }
        sm.Device.Map.AreaName = convertedList
    }
}

func (sm *StateManager) updateSysData(message *model.LubaMsg) {
    switch message.Sys.Type {
    case "system_update_buf":
        sm.Device.Buffer(message.Sys.UpdateBuf)
    case "toapp_report_data":
        sm.Device.UpdateReportData(message.Sys.ReportData)
    case "mow_to_app_info":
        sm.Device.MowInfo(message.Sys.MowInfo)
    case "system_tard_state_tunnel":
        sm.Device.RunStateUpdate(message.Sys.TardStateTunnel)
    case "todev_time_ctrl_light":
        ctrlLight := message.Sys.TimeCtrlLight
        sideLed := model.SideLightFromDict(ctrlLight.ToDict())
        sm.Device.MowerState.SideLed = sideLed
    case "device_product_type_info":
        deviceProductType := message.Sys.DeviceProductTypeInfo
        sm.Device.MowerState.ModelID = deviceProductType.MainProductType
    }
}

func (sm *StateManager) updateDriverData(message *model.LubaMsg) {
    // Implement driver data update logic
}

func (sm *StateManager) updateNetData(message *model.LubaMsg) {
    switch message.Net.Type {
    case "toapp_wifi_iot_status":
        wifiIotStatus := message.Net.WifiIotStatus
        sm.Device.MowerState.ProductKey = wifiIotStatus.ProductKey
    }
}

func (sm *StateManager) updateMulData(message *model.LubaMsg) {
    // Implement mul data update logic
}

func (sm *StateManager) updateOtaData(message *model.LubaMsg) {
    // Implement OTA data update logic
}
